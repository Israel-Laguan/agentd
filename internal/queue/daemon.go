package queue

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"agentd/internal/config"
	"agentd/internal/frontdesk"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	qw "agentd/internal/queue/worker"

	"github.com/robfig/cron/v3"
)

type Daemon struct {
	store                models.KanbanStore
	worker               *qw.Worker
	intake               *frontdesk.IntakeProcessor
	breaker              *safety.CircuitBreaker
	sem                  *safety.Semaphore
	probe                safety.PIDProbe
	sink                 models.EventSink
	taskInterval         time.Duration
	maxTaskInterval      time.Duration
	taskDeadline         time.Duration
	intakeEvery          time.Duration
	heartbeatInterval    time.Duration
	staleAfter           time.Duration
	handoffAfter         time.Duration
	diskWatchdogEvery    time.Duration
	diskWatchdogSchedule cron.Schedule
	diskFreeThreshold    float64
	diskCheckPath        string
	diskStat             func(string) (float64, error)
	librarian            *memory.Librarian
	dreamer              *memory.DreamAgent
	curatorEvery         time.Duration
	curatorSchedule      cron.Schedule
	dreamEvery           time.Duration
	dreamSchedule        cron.Schedule
	channel                  Channel
	queuedReconcileAfter     time.Duration
	rateLimitedRequeueAfter  time.Duration
	wg                       sync.WaitGroup
}

type DaemonOptions struct {
	MaxWorkers           int
	TaskInterval         time.Duration
	MaxTaskInterval      time.Duration
	TaskDeadline         time.Duration
	IntakeInterval       time.Duration
	HeartbeatInterval    time.Duration
	StaleAfter           time.Duration
	HandoffAfter         time.Duration
	DiskWatchdogEvery    time.Duration
	DiskWatchdogSchedule cron.Schedule
	DiskFreeThreshold    float64
	DiskCheckPath        string
	Probe                safety.PIDProbe
	Librarian            *memory.Librarian
	Dreamer              *memory.DreamAgent
	CuratorEvery         time.Duration
	CuratorSchedule      cron.Schedule
	DreamEvery           time.Duration
	DreamSchedule        cron.Schedule
	Channel                 Channel
	QueuedReconcileAfter    time.Duration
	RateLimitedRequeueAfter time.Duration
}

func NewDaemon(
	store models.KanbanStore,
	worker *qw.Worker,
	intake *frontdesk.IntakeProcessor,
	breaker *safety.CircuitBreaker,
	sink models.EventSink,
	opts DaemonOptions,
) *Daemon {
	opts = normalizeDaemonOptions(opts)
	return &Daemon{
		store: store, worker: worker, intake: intake, breaker: breaker, sink: sink,
		sem: safety.NewSemaphore(opts.MaxWorkers), probe: opts.Probe,
		taskInterval: opts.TaskInterval, maxTaskInterval: opts.MaxTaskInterval,
		taskDeadline: opts.TaskDeadline, intakeEvery: opts.IntakeInterval,
		heartbeatInterval: opts.HeartbeatInterval, staleAfter: opts.StaleAfter, handoffAfter: opts.HandoffAfter,
		diskWatchdogEvery: opts.DiskWatchdogEvery, diskWatchdogSchedule: opts.DiskWatchdogSchedule,
		diskFreeThreshold: opts.DiskFreeThreshold, diskCheckPath: opts.DiskCheckPath,
		diskStat:  safety.DiskFreePercent,
		librarian: opts.Librarian, dreamer: opts.Dreamer,
		curatorEvery: opts.CuratorEvery, curatorSchedule: opts.CuratorSchedule,
		dreamEvery: opts.DreamEvery, dreamSchedule: opts.DreamSchedule,
		channel:                 opts.Channel,
		queuedReconcileAfter:    opts.QueuedReconcileAfter,
		rateLimitedRequeueAfter: opts.RateLimitedRequeueAfter,
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	if err := recovery.BootReconcile(ctx, d.store, d.probe, d.sink); err != nil {
		return err
	}
	logDaemonError("orphaned queued reconcile failed", d.reconcileOrphanedQueued(ctx))
	d.wg.Add(7)
	go d.taskLoop(ctx)
	go d.intakeLoop(ctx)
	go d.heartbeatReconcileLoop(ctx)
	go d.queuedReconcileLoop(ctx)
	go d.diskWatchdogLoop(ctx)
	go d.memoryCuratorLoop(ctx)
	go d.dreamLoop(ctx)
	<-ctx.Done()
	d.wg.Wait()
	return nil
}

func (d *Daemon) Stop() error { return nil }

func normalizeDaemonOptions(opts DaemonOptions) DaemonOptions {
	normalizeWorkerOptions(&opts)
	normalizeIntervals(&opts)
	normalizeDiskOptions(&opts)
	normalizeMemorySchedules(&opts)
	if opts.Probe == nil {
		opts.Probe = safety.GopsutilProbe{}
	}
	return opts
}

func normalizeWorkerOptions(opts *DaemonOptions) {
	if opts.MaxWorkers < 1 {
		opts.MaxWorkers = runtime.NumCPU() - 2
	}
	if opts.MaxWorkers < 1 {
		opts.MaxWorkers = 1
	}
}

func normalizeIntervals(opts *DaemonOptions) {
	if opts.TaskInterval <= 0 {
		opts.TaskInterval = config.DefaultCronSchedule.TaskDispatch
	}
	if opts.MaxTaskInterval <= 0 {
		opts.MaxTaskInterval = 10 * time.Second
	}
	if opts.TaskDeadline <= 0 {
		opts.TaskDeadline = 10 * time.Minute
	}
	if opts.IntakeInterval <= 0 {
		opts.IntakeInterval = config.DefaultCronSchedule.Intake
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = config.DefaultCronSchedule.Heartbeat
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = 2 * time.Minute
	}
	if opts.HandoffAfter <= 0 {
		opts.HandoffAfter = 2 * time.Minute
	}
}

func normalizeDiskOptions(opts *DaemonOptions) {
	if opts.DiskWatchdogSchedule == nil {
		opts.DiskWatchdogSchedule = config.DefaultCronSchedule.DiskWatchdog.Schedule
	}
	if opts.DiskFreeThreshold <= 0 {
		opts.DiskFreeThreshold = 10.0
	}
	if opts.DiskCheckPath == "" {
		opts.DiskCheckPath = "."
	}
}

func normalizeMemorySchedules(opts *DaemonOptions) {
	if opts.CuratorSchedule == nil {
		opts.CuratorSchedule = config.DefaultCronSchedule.MemoryCurator.Schedule
	}
	if opts.DreamSchedule == nil {
		opts.DreamSchedule = config.DefaultCronSchedule.Dream.Schedule
	}
}

func logDaemonError(msg string, err error) {
	if err != nil {
		slog.Error(msg, "error", err)
	}
}
