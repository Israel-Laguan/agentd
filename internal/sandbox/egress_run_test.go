package sandbox

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

// A run gets its own home and egress on a copy of the executor.
func TestStartEgressRunBindsItsOwnHomeOnACopy(t *testing.T) {
	t.Parallel()
	base := &BashExecutor{Root: t.TempDir()}
	run, err := base.StartEgressRun([]string{"model.test:443"}, "/run/mcp.sock", "/opt/agentd", "/opt/agent")
	if err != nil {
		t.Fatal(err)
	}
	defer run.Close()
	g := run.Executor.Egress
	if base.Egress != nil || g == nil || g.Home != run.Home || g.MCPSocket != "/run/mcp.sock" ||
		g.Forwarder != "/opt/agentd" || g.Executable != "/opt/agent" || run.Executor.Root != base.Root {
		t.Fatalf("base egress %+v, run egress %+v", base.Egress, g)
	}
	if info, err := os.Stat(run.Home); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("home %s: %v, %v", run.Home, info, err)
	}
}

// The run's proxy refuses a host its preset does not list, and Close leaves
// nothing behind.
func TestStartEgressRunRefusesUnlistedHostsAndCleansUp(t *testing.T) {
	t.Parallel()
	run, err := (&BashExecutor{}).StartEgressRun([]string{"model.test:443"}, "", "/opt/agentd", "/opt/agent")
	if err != nil {
		t.Fatal(err)
	}
	socket := run.Executor.Egress.ProxySocket
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprint(conn, "CONNECT other.test:443 HTTP/1.1\r\nHost: other.test:443\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	_ = conn.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CONNECT to an unlisted host: %s; want 403", resp.Status)
	}
	run.Close()
	for _, path := range []string{run.Home, socket} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived Close: %v", path, err)
		}
	}
}
