package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
	var processID atomic.Int32
	markTimedOut := func() {
		timeoutOnce.Do(func() {
			close(timedOut)
			cancel()
			if pid := processID.Load(); pid > 0 {
				_ = terminateProcessGroup(int(pid), e.killGrace())
			}
		})
	}
	stdout = e.watch(stdout, markTimedOut)
	stderr = e.watch(stderr, markTimedOut)
	stopWallTimeout := e.startWallTimeout(payload, markTimedOut)
	defer stopWallTimeout()
	output := newCommandOutput(e.maxLogBytes(), e.scrubber())
	output.start(execCtx, e.Sink, payload, stdout, stderr)
	if err := cmd.Start(); err != nil {
		cancel()
		output.wg.Wait()
		return Result{}, fmt.Errorf("start command: %w", err)
	}
	processID.Store(int32(cmd.Process.Pid))
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- waitCommand(cmd, timedOut, e.killGrace())
	}()
	output.wg.Wait()
	waitErr := <-waitDone
	if drainErr := output.drainError(); drainErr != nil {
		return Result{}, fmt.Errorf("drain output: %w", drainErr)
	}
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

var sudoPattern = regexp.MustCompile(`(?:^|&&|\|\||[;|])\s*sudo\b`)

func containsSudo(command string) bool {
	return sudoPattern.MatchString(command)
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
