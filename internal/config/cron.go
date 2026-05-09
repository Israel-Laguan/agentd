package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const defaultCronFile = `# agentd.crontab - schedule for agentd background jobs.
# Format: m h dom mon dow <job>      (Vixie crontab, minute resolution)
#         @every <duration> <job>    (robfig/cron descriptor, sub-minute)
# Recognized jobs:
#   task-dispatch    claim and run READY tasks
#   intake           process unprocessed HUMAN comments
#   heartbeat        refresh RUNNING task heartbeats
#   disk-watchdog    alert on low free disk
#   memory-curator   archive logs / ingest memories

@every 3s       task-dispatch
@every 5s       intake
@every 30s      heartbeat
*/10 * * * *    disk-watchdog
0 * * * *       memory-curator
`

// CronJob is a parsed crontab entry for a named agentd background job.
type CronJob struct {
	Name     string
	Spec     string
	Every    time.Duration
	Schedule cron.Schedule
}

// CronSchedule contains agentd's parsed background job schedule.
type CronSchedule struct {
	Path          string
	TaskDispatch  time.Duration
	Intake        time.Duration
	Heartbeat     time.Duration
	DiskWatchdog  CronJob
	MemoryCurator CronJob
	Dream         CronJob
}

// DefaultCronSchedule preserves the current daemon cadence when no crontab exists.
var DefaultCronSchedule = CronSchedule{
	TaskDispatch:  3 * time.Second,
	Intake:        5 * time.Second,
	Heartbeat:     30 * time.Second,
	DiskWatchdog:  CronJob{Name: "disk-watchdog", Spec: "*/10 * * * *"},
	MemoryCurator: CronJob{Name: "memory-curator", Spec: "0 * * * *"},
	Dream:         CronJob{Name: "dream", Spec: "0 3 * * *"},
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// LoadCron reads an agentd crontab file. A missing file returns defaults.
func LoadCron(path string) (CronSchedule, error) {
	schedule := DefaultCronSchedule
	schedule.Path = path
	setDefaultCronSchedules(&schedule)

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return schedule, nil
		}
		return CronSchedule{}, fmt.Errorf("open cron file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := applyCronLine(&schedule, line); err != nil {
			return CronSchedule{}, fmt.Errorf("parse cron line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return CronSchedule{}, fmt.Errorf("read cron file: %w", err)
	}
	return schedule, nil
}

// WriteDefaultCron writes the sample crontab without overwriting user edits.
func WriteDefaultCron(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("create cron file: %w", err)
	}
	defer file.Close() //nolint:errcheck
	if _, err := file.WriteString(defaultCronFile); err != nil {
		return fmt.Errorf("write cron file: %w", err)
	}
	return nil
}

func setDefaultCronSchedules(schedule *CronSchedule) {
	schedule.DiskWatchdog.Schedule = mustParseCron(DefaultCronSchedule.DiskWatchdog.Spec)
	schedule.MemoryCurator.Schedule = mustParseCron(DefaultCronSchedule.MemoryCurator.Spec)
	schedule.Dream.Schedule = mustParseCron(DefaultCronSchedule.Dream.Spec)
}

func applyCronLine(schedule *CronSchedule, line string) error {
	spec, job, err := splitCronLine(line)
	if err != nil {
		return err
	}
	parsed, err := cronParser.Parse(spec)
	if err != nil {
		return fmt.Errorf("parse schedule %q: %w", spec, err)
	}
	entry := CronJob{Name: job, Spec: spec, Schedule: parsed}
	if strings.HasPrefix(spec, "@every ") {
		entry.Every, err = time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(spec, "@every ")))
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", spec, err)
		}
	}
	return applyCronJob(schedule, entry)
}

func splitCronLine(line string) (string, string, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("expected schedule and job name")
	}
	if strings.HasPrefix(fields[0], "@") {
		if fields[0] == "@every" {
			if len(fields) != 3 {
				return "", "", fmt.Errorf("@every lines must be: @every <duration> <job>")
			}
			return fields[0] + " " + fields[1], fields[2], nil
		}
		if len(fields) != 2 {
			return "", "", fmt.Errorf("descriptor lines must be: <descriptor> <job>")
		}
		return fields[0], fields[1], nil
	}
	if len(fields) != 6 {
		return "", "", fmt.Errorf("standard cron lines must be: m h dom mon dow <job>")
	}
	return strings.Join(fields[:5], " "), fields[5], nil
}

func applyCronJob(schedule *CronSchedule, job CronJob) error {
	switch job.Name {
	case "task-dispatch":
		return applyIntervalJob(&schedule.TaskDispatch, job)
	case "intake":
		return applyIntervalJob(&schedule.Intake, job)
	case "heartbeat":
		return applyIntervalJob(&schedule.Heartbeat, job)
	case "disk-watchdog":
		warnDuplicateCron(schedule.DiskWatchdog, job)
		schedule.DiskWatchdog = job
	case "memory-curator":
		warnDuplicateCron(schedule.MemoryCurator, job)
		schedule.MemoryCurator = job
	case "dream":
		warnDuplicateCron(schedule.Dream, job)
		schedule.Dream = job
	default:
		slog.Warn("skipping unknown cron job", "job", job.Name)
	}
	return nil
}

func applyIntervalJob(target *time.Duration, job CronJob) error {
	if job.Every <= 0 {
		return fmt.Errorf("%s must use @every <duration>", job.Name)
	}
	if *target > 0 && *target != job.Every {
		slog.Warn("overriding cron job", "job", job.Name)
	}
	*target = job.Every
	return nil
}

func warnDuplicateCron(existing CronJob, replacement CronJob) {
	if existing.Spec != "" && existing.Spec != replacement.Spec {
		slog.Warn("overriding cron job", "job", replacement.Name)
	}
}

func mustParseCron(spec string) cron.Schedule {
	parsed, err := cronParser.Parse(spec)
	if err != nil {
		panic(err)
	}
	return parsed
}
