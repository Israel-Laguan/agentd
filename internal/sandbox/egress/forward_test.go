package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the forwarder: `<test binary> jail-forward ...` runs
// Forward, so the test can start it under a parent shell like the jail does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "jail-forward" {
		if err := Forward(context.Background(), os.Stdout, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestForwardCopiesToUnixSocketAndExitsWithParent(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "mcp.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go echo(ln)

	forwarder, exitParent := startUnderShell(t, "127.0.0.1:0="+socket)
	line, err := forwarder.ReadString('\n')
	fields := strings.Fields(line)
	if err != nil || len(fields) != 2 || fields[0] != "ready" {
		t.Fatalf("ready line = %q, err = %v; want \"ready <addr>\"", line, err)
	}
	conn, err := net.Dial("tcp", fields[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "ping" {
		t.Fatalf("round trip = %q, err = %v; want ping", got, err)
	}
	_ = conn.Close()

	exitParent()
	// The forwarder holds the pipe's write end until it exits.
	exited := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, forwarder); close(exited) }()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("jail-forward outlived its parent shell")
	}
}

// startUnderShell starts the forwarder the way the jail's launcher does: in
// the background of a shell, which lives until exitParent is called. It
// returns the forwarder's stdout.
func startUnderShell(t *testing.T, pair string) (forwarder *bufio.Reader, exitParent func()) {
	t.Helper()
	shell := exec.Command("bash", "-c", `"$0" jail-forward "$1" & read -r _`, os.Args[0], pair)
	stdin, err := shell.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	shell.Stdout, shell.Stderr = w, os.Stderr
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	exitParent = func() { _ = stdin.Close(); _ = shell.Wait() }
	t.Cleanup(func() { exitParent(); _ = out.Close() })
	return bufio.NewReader(out), exitParent
}

func TestForwardRejectsMalformedPair(t *testing.T) {
	for _, args := range [][]string{nil, {"127.0.0.1:3128"}, {"=/run/x.sock"}} {
		if err := Forward(context.Background(), io.Discard, args); err == nil {
			t.Fatalf("Forward(%q) = nil, want an error", args)
		}
	}
}
