package sandbox

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"agentd/internal/models"
)

const defaultInactivityLimit = 60 * time.Second
const defaultMaxLogBytes = 5 * 1024 * 1024
const defaultKillGrace = 2 * time.Second

type BashExecutor struct {
	Root        string
	Sink        models.EventSink
	Inactivity  time.Duration
	KillGrace   time.Duration
	MaxLogBytes int
	Scrubber    Scrubber
	Limits      ResourceLimits
}

var _ Executor = (*BashExecutor)(nil)

func (e *BashExecutor) Execute(ctx context.Context, payload Payload) (Result, error) {
	if strings.TrimSpace(payload.Command) == "" {
		return Result{}, errors.New("command is required")
	}
	workspace, err := JailPath(e.Root, payload.WorkspacePath)
	if err != nil {
		return Result{}, err
	}
	if containsSudo(payload.Command) {
		emitLine(ctx, e.Sink, payload, "SANDBOX_VIOLATION", e.scrub("sudo command blocked"))
		return Result{ExitCode: -1, Stderr: e.scrub("sudo command blocked")}, fmt.Errorf("%w: sudo is not allowed", models.ErrSandboxViolation)
	}
	if err := validateCommandPaths(payload.Command, workspace); err != nil {
		emitLine(ctx, e.Sink, payload, "SANDBOX_VIOLATION", e.scrub("directory escape attempt blocked"))
		return Result{ExitCode: -1}, err
	}
	return e.run(ctx, workspace, payload)
}

func (e *BashExecutor) run(ctx context.Context, workspace string, payload Payload) (Result, error) {
	started := time.Now()
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(execCtx, "bash", "-c", withResourceLimits(payload.Command, e.resourceLimits()))
	cmd.Dir = workspace
	cmd.Env = nonInheritedEnv(payload.EnvVars)
	cmd.Stdin = strings.NewReader("")
	configureProcess(cmd, e.resourceLimits())
	stdout, stderr, err := commandPipes(cmd)
	if err != nil {
		return Result{}, err
	}
	timedOut := make(chan struct{})
	var timeoutOnce sync.Once
	processID := 0
	markTimedOut := func() {
		timeoutOnce.Do(func() {
			close(timedOut)
			cancel()
			if processID > 0 {
				_ = terminateProcessGroup(processID, e.killGrace())
			}
		})
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start command: %w", err)
	}
	processID = cmd.Process.Pid
	stdout = e.watch(stdout, markTimedOut)
	stderr = e.watch(stderr, markTimedOut)
	stopWallTimeout := e.startWallTimeout(payload, markTimedOut)
	defer stopWallTimeout()
	output := newCommandOutput(e.maxLogBytes(), e.scrubber())
	output.start(execCtx, e.Sink, payload, stdout, stderr)
	waitErr := waitCommand(cmd, timedOut, e.killGrace())
	output.wg.Wait()
	result := output.result(cmd, started, hasTimedOut(timedOut))
	return result, finishError(waitErr, result.TimedOut)
}

func (e *BashExecutor) watch(reader io.Reader, markTimedOut func()) io.Reader {
	limit := e.Inactivity
	if limit <= 0 {
		limit = defaultInactivityLimit
	}
	return newInactivityReader(reader, limit, markTimedOut)
}

func (e *BashExecutor) startWallTimeout(payload Payload, markTimedOut func()) func() {
	limit := payload.WallTimeout
	if limit <= 0 && payload.TimeoutLimit > 0 {
		limit = time.Duration(payload.TimeoutLimit) * time.Second
	}
	if limit <= 0 {
		return func() {}
	}
	timer := time.AfterFunc(limit, markTimedOut)
	return func() { timer.Stop() }
}

func commandPipes(cmd *exec.Cmd) (io.Reader, io.Reader, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}
	return stdout, stderr, nil
}

func waitCommand(cmd *exec.Cmd, timedOut <-chan struct{}, grace time.Duration) error {
	err := cmd.Wait()
	if hasTimedOut(timedOut) && cmd.Process != nil {
		_ = terminateProcessGroup(cmd.Process.Pid, grace)
	}
	return err
}

func finishError(err error, timedOut bool) error {
	if timedOut {
		return fmt.Errorf("%w: no output within limit", models.ErrExecutionTimeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
		return fmt.Errorf("wait command: %w", err)
	}
	return nil
}

func hasTimedOut(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

type commandOutput struct {
	stdout *headTailBuffer
	stderr *headTailBuffer
	scrub  Scrubber
	wg     sync.WaitGroup
}

func newCommandOutput(limit int, scrubber Scrubber) commandOutput {
	return commandOutput{
		stdout: newHeadTailBuffer(limit),
		stderr: newHeadTailBuffer(limit),
		scrub:  scrubber,
	}
}

func (o *commandOutput) start(ctx context.Context, sink models.EventSink, payload Payload, stdout, stderr io.Reader) {
	o.wg.Add(2)
	go o.scan(ctx, sink, payload, models.EventType("LOG_CHUNK"), stdout, o.stdout)
	go o.scan(ctx, sink, payload, models.EventType("LOG_CHUNK"), stderr, o.stderr)
}

func (o *commandOutput) scan(
	ctx context.Context,
	sink models.EventSink,
	payload Payload,
	eventType models.EventType,
	reader io.Reader,
	buf *headTailBuffer,
) {
	defer o.wg.Done()
	scanner := bufio.NewScanner(reader)
	maxTokenSize := 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxTokenSize)
	for scanner.Scan() {
		line := o.scrubLine(scanner.Text())
		buf.WriteString(line + "\n")
		emitLine(ctx, sink, payload, eventType, line)
	}
}

func (o *commandOutput) result(cmd *exec.Cmd, started time.Time, timedOut bool) Result {
	exitCode := commandExitCode(cmd)
	return Result{
		ExitCode:    exitCode,
		Success:     exitCode == 0 && !timedOut,
		Stdout:      o.scrubLine(o.stdout.String()),
		Stderr:      o.scrubLine(o.stderr.String()),
		Duration:    time.Since(started),
		TimedOut:    timedOut,
		OSProcessID: cmd.Process.Pid,
	}
}

var sudoPattern = regexp.MustCompile(`(?:^|&&|\|\||[;|])\s*sudo\b`)

func containsSudo(command string) bool {
	return sudoPattern.MatchString(command)
}

func commandExitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func (o *commandOutput) scrubLine(value string) string {
	if o.scrub == nil {
		return value
	}
	return o.scrub.Scrub(value)
}

func (e *BashExecutor) scrub(value string) string {
	scrubber := e.scrubber()
	if scrubber == nil {
		return value
	}
	return scrubber.Scrub(value)
}

func (e *BashExecutor) scrubber() Scrubber {
	if e.Scrubber == nil {
		return NewScrubber(nil)
	}
	return e.Scrubber
}

func (e *BashExecutor) maxLogBytes() int {
	if e.MaxLogBytes <= 0 {
		return defaultMaxLogBytes
	}
	return e.MaxLogBytes
}

func (e *BashExecutor) killGrace() time.Duration {
	if e.KillGrace <= 0 {
		return defaultKillGrace
	}
	return e.KillGrace
}

func (e *BashExecutor) resourceLimits() ResourceLimits {
	limits := e.Limits
	if limits.AddressSpaceBytes == 0 {
		limits.AddressSpaceBytes = 2 * 1024 * 1024 * 1024
	}
	if limits.CPUSeconds == 0 {
		limits.CPUSeconds = 600
	}
	if limits.OpenFiles == 0 {
		limits.OpenFiles = 1024
	}
	if limits.Processes == 0 {
		limits.Processes = 256
	}
	return limits
}

func nonInheritedEnv(env []string) []string {
	if env == nil {
		return []string{}
	}
	return env
}

func emitLine(ctx context.Context, sink models.EventSink, payload Payload, eventType models.EventType, line string) {
	if sink == nil {
		return
	}
	_ = sink.Emit(ctx, models.Event{
		ProjectID: payload.ProjectID,
		TaskID:    sql.NullString{String: payload.TaskID, Valid: payload.TaskID != ""},
		Type:      eventType,
		Payload:   line,
	})
}
