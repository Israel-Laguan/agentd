// Package egress is the jail's only network path for an external agent
// (T-042, SP-025): a host-side CONNECT proxy that allows a fixed list of
// host:port pairs, and the in-jail forwarder (agentd jail-forward) that
// exposes the proxy's unix socket on the jail's own loopback.
package egress

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"syscall"
	"time"
)

// errLoopback marks a dial refused because the address is the host itself.
var errLoopback = errors.New("address is the host's loopback")

// Proxy is an HTTP CONNECT proxy for one run, served on a unix socket bound
// into the jail. It tunnels a CONNECT to a host:port on Allow and answers 403
// to everything else, the host's loopback included even when listed: the
// daemon's board API and MCP server listen there.
type Proxy struct {
	// Allow is the host:port list a CONNECT may reach, compared case-insensitively.
	Allow []string
	// Dial opens the upstream connection. Nil uses a dialer that refuses
	// loopback and unspecified addresses after name resolution; tests set it
	// to route a listed name to a local server.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
}

// Serve answers CONNECT requests on ln until ln is closed.
func (p *Proxy) Serve(ln net.Listener) error {
	server := &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	return server.Serve(ln)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if r.Method != http.MethodConnect {
		refuse(w, r.Method, target, "only CONNECT is proxied")
		return
	}
	listed := slices.IndexFunc(p.Allow, func(allowed string) bool { return strings.EqualFold(allowed, target) })
	if listed < 0 {
		refuse(w, r.Method, target, "not on the egress allowlist")
		return
	}
	upstream, err := p.dial(r.Context(), p.Allow[listed])
	if errors.Is(err, errLoopback) {
		refuse(w, r.Method, target, err.Error())
		return
	}
	if err != nil {
		slog.Warn("egress: upstream dial failed", "target", target, "error", err)
		http.Error(w, "egress: "+target+" unreachable", http.StatusBadGateway)
		return
	}
	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = upstream.Close()
		http.Error(w, "egress: tunnel unavailable", http.StatusInternalServerError)
		return
	}
	_, err = io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	if n := buffered.Reader.Buffered(); err == nil && n > 0 {
		early, _ := buffered.Peek(n)
		_, err = upstream.Write(early)
	}
	if err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	splice(client, upstream)
}

// refuse answers 403 and logs the target, so an operator sees which hosts an
// agent wanted (SP-025 found opencode's model catalog this way).
func refuse(w http.ResponseWriter, method, target, reason string) {
	slog.Warn("egress: request refused", "method", method, "target", target, "reason", reason)
	http.Error(w, "egress: "+target+" refused: "+reason, http.StatusForbidden)
}

func (p *Proxy) dial(ctx context.Context, target string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, "tcp", target)
	}
	dialer := net.Dialer{Timeout: 30 * time.Second, Control: refuseLoopback}
	return dialer.DialContext(ctx, "tcp", target)
}

// refuseLoopback runs on each resolved address, so a listed name that
// resolves to the host (localhost, a rebinding record) is refused like a
// literal 127.0.0.1.
func refuseLoopback(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if ip = ip.Unmap(); ip.IsLoopback() || ip.IsUnspecified() {
		return errLoopback
	}
	return nil
}

// splice copies both ways until each side has finished sending, passing a
// half-close through so request-then-EOF protocols still work. A failed copy
// (a reset) closes both connections, so the other direction cannot hang on a
// peer that never closes.
func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	half := func(dst, src net.Conn) {
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			_ = src.Close()
		} else if closer, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- struct{}{}
	}
	go half(a, b)
	go half(b, a)
	<-done
	<-done
	_ = a.Close()
	_ = b.Close()
}
