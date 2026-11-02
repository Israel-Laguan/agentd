package egress

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProxyTunnelsAllowedConnect(t *testing.T) {
	upstream := listenEcho(t)
	proxy := &Proxy{
		Allow: []string{"api.example:443"},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "api.example:443" {
				return nil, fmt.Errorf("dialed %s, want api.example:443", address)
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, upstream)
		},
	}
	conn, reply, status := connect(t, serve(t, proxy), "API.example:443")
	if status != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", status)
	}
	roundTrip(t, conn, reply)
}

func TestProxyRefusesUnlistedHost(t *testing.T) {
	logs := captureLogs(t)
	proxy := &Proxy{Allow: []string{"api.example:443"}, Dial: func(context.Context, string, string) (net.Conn, error) {
		t.Error("an unlisted host was dialed")
		return nil, fmt.Errorf("unexpected dial")
	}}
	_, _, status := connect(t, serve(t, proxy), "evil.example:443")
	if status != http.StatusForbidden {
		t.Fatalf("CONNECT status = %d, want 403", status)
	}
	if line := logs.String(); !strings.Contains(line, "evil.example:443") || !strings.Contains(line, "refused") {
		t.Fatalf("log = %q, want a refusal line naming evil.example:443", line)
	}
}

// Only CONNECT is proxied: a plain-HTTP request to a listed host is refused,
// and the log says why.
func TestProxyRefusesPlainHTTPToListedHost(t *testing.T) {
	logs := captureLogs(t)
	conn, err := net.Dial("unix", serve(t, &Proxy{Allow: []string{"api.example:443"}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, "GET http://api.example:443/ HTTP/1.1\r\nHost: api.example:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(logs.String(), "only CONNECT") {
		t.Fatalf("GET status = %d, log = %q; want 403 and the method as the reason", resp.StatusCode, logs.String())
	}
}

// The daemon's board API and MCP server listen on the host's loopback, so
// the proxy must never reach it, even when the list names it: a literal
// address, a name that resolves there, or an unspecified address.
func TestProxyRefusesHostLoopbackEvenWhenListed(t *testing.T) {
	_, port, err := net.SplitHostPort(listenEcho(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"127.0.0.1:" + port, "localhost:" + port, "[::ffff:127.0.0.1]:" + port, "0.0.0.0:" + port, "[::]:" + port} {
		t.Run(target, func(t *testing.T) {
			logs := captureLogs(t)
			_, _, status := connect(t, serve(t, &Proxy{Allow: []string{target}}), target)
			if status != http.StatusForbidden {
				t.Fatalf("CONNECT %s status = %d, want 403", target, status)
			}
			if !strings.Contains(logs.String(), "refused") {
				t.Fatalf("log = %q, want a refusal line", logs.String())
			}
		})
	}
}

// serve runs p on a fresh unix socket and returns its path.
func serve(t *testing.T, p *Proxy) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "egress.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = p.Serve(ln) }()
	return socket
}

// connect sends one CONNECT through the proxy socket and returns the
// connection, a reader positioned after the response, and its status.
func connect(t *testing.T, socket, target string) (net.Conn, *bufio.Reader, int) {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		t.Fatal(err)
	}
	reply := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reply, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
	}
	return conn, reply, resp.StatusCode
}

// roundTrip sends ping on conn and expects it echoed back through reply,
// failing rather than hanging when nothing answers.
func roundTrip(t *testing.T, conn net.Conn, reply io.Reader) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(reply, got); err != nil || string(got) != "ping" {
		t.Fatalf("round trip = %q, err = %v; want ping", got, err)
	}
}

// listenEcho starts a TCP echo server on the host loopback.
func listenEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go echo(ln)
	return ln.Addr().String()
}

func echo(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = conn.Close() }()
			_, _ = io.Copy(conn, conn)
		}()
	}
}

// lockedBuffer is written by the proxy's goroutines and read by the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs points the default logger at a buffer for one test, so callers
// must not run in parallel.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return logs
}
