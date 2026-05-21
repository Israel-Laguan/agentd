package sandbox

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agentd/internal/models"
)

type commandOutput struct {
	stdout   *headTailBuffer
	stderr   *headTailBuffer
	scrub    Scrubber
	wg       sync.WaitGroup
	drainMu  sync.Mutex
	drainErr error
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
	if err := scanner.Err(); err != nil && !isBenignDrainErr(err) {
		o.recordDrainErr(fmt.Errorf("read stream: %w", err))
	}
}

func isBenignDrainErr(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "file already closed") || strings.Contains(msg, "broken pipe")
}

func (o *commandOutput) recordDrainErr(err error) {
	o.drainMu.Lock()
	defer o.drainMu.Unlock()
	o.drainErr = errors.Join(o.drainErr, err)
}

func (o *commandOutput) drainError() error {
	o.drainMu.Lock()
	defer o.drainMu.Unlock()
	return o.drainErr
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
