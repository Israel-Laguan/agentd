package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// Forward is the in-jail half of the bridge, run as `agentd jail-forward
// ADDR=SOCKET...`. The jail's network is its own loopback (B-086), so a TCP
// client such as HTTPS_PROXY=http://127.0.0.1:3128 cannot reach a host
// socket by itself: Forward listens on each ADDR and copies every connection
// to a new connection on its unix SOCKET. Once every listener is bound it
// writes "ready ADDR..." to ready; the jail's launcher waits for that line
// before it starts the agent. It returns when ctx ends or its parent exits,
// so it never outlives the launcher shell.
func Forward(ctx context.Context, ready io.Writer, pairs []string) error {
	if len(pairs) == 0 {
		return errors.New("jail-forward: want at least one ADDR=SOCKET")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cancelWhenOrphaned(ctx, cancel, os.Getppid())
	bound := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		addr, socket, ok := strings.Cut(pair, "=")
		if !ok || addr == "" || socket == "" {
			return fmt.Errorf("jail-forward: %q is not ADDR=SOCKET", pair)
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("jail-forward: %w", err)
		}
		context.AfterFunc(ctx, func() { _ = ln.Close() })
		go forwardEach(ln, socket)
		bound = append(bound, ln.Addr().String())
	}
	if _, err := fmt.Fprintln(ready, "ready", strings.Join(bound, " ")); err != nil {
		return fmt.Errorf("jail-forward: %w", err)
	}
	<-ctx.Done()
	return nil
}

func forwardEach(ln net.Listener, socket string) {
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			upstream, err := net.Dial("unix", socket)
			if err != nil {
				slog.Warn("jail-forward: dial failed", "socket", socket, "error", err)
				_ = client.Close()
				return
			}
			splice(client, upstream)
		}()
	}
}

// cancelWhenOrphaned cancels once the parent is gone, which a reparented
// process sees as a new parent PID. Polling stays portable, where
// PR_SET_PDEATHSIG is Linux-only and tied to the thread that set it.
func cancelWhenOrphaned(ctx context.Context, cancel context.CancelFunc, parent int) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if os.Getppid() != parent {
				cancel()
				return
			}
		}
	}
}
