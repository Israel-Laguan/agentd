package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// B-086: the jail shared the host network namespace, so a jailed command
// reached any service on the host loopback (the dev daemon's board API among
// them) and the internet. A jailed connect to a host loopback listener must
// fail.
func TestBubblewrapRefusesHostLoopback(t *testing.T) {
	requireJail(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go acceptAndClose(listener)
	port := listener.Addr().(*net.TCPAddr).Port

	exec, workspace := testExecutor(t, nil)
	// The lexical filter refuses a literal /dev/tcp path, but it is no barrier:
	// assemble the path at run time so the jail itself must refuse the connect.
	target := slashFree(fmt.Sprintf("/dev/tcp/127.0.0.1/%d", port))
	command := slashSeed() + "bash -c \"exec 3<>" + target + "\""
	result, err := exec.Execute(context.Background(), testPayload(workspace, command))
	if err == nil && result.Success {
		t.Fatalf("jailed connect to host loopback port %d succeeded; want refused (stdout %q, stderr %q)",
			port, result.Stdout, result.Stderr)
	}
	if IsSandboxViolation(err) {
		t.Fatalf("Execute() = %v; want the jail to refuse the connect, not the lexical filter", err)
	}
}

// Unsharing the network must not cost the workspace: a jailed command still
// reads and writes workspace-local files.
func TestBubblewrapWithoutNetworkKeepsWorkspaceFiles(t *testing.T) {
	requireJail(t)
	exec, workspace := testExecutor(t, nil)
	if err := os.WriteFile(filepath.Join(workspace, "in.txt"), []byte("local data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Execute(context.Background(), testPayload(workspace, "cat in.txt > out.txt && cat out.txt"))
	if err != nil || !result.Success {
		t.Fatalf("Execute() err = %v, success = %v, stderr = %q", err, result.Success, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "local data") {
		t.Fatalf("stdout = %q, want the workspace file's content", result.Stdout)
	}
	if _, err := os.Stat(filepath.Join(workspace, "out.txt")); err != nil {
		t.Fatalf("out.txt not written to the workspace: %v", err)
	}
}

// go test must still run through the toolchain bind (B-081) with no network.
// The module's own test listens on 127.0.0.1: the jail's private loopback
// stays up, so a project's local test servers keep working.
func TestBubblewrapGoTestRunsWithoutNetwork(t *testing.T) {
	requireJail(t)
	goroot := hostGoroot(t)
	exec, workspace := testExecutor(t, nil)
	exec.ToolchainPaths = []string{goroot}
	exec.Inactivity = 2 * time.Minute // a cold build cache compiles the testing package silently
	files := map[string]string{
		"go.mod": "module jailprobe\n\ngo 1.21\n",
		"probe_test.go": "package jailprobe\n\nimport (\n\t\"net\"\n\t\"testing\"\n)\n\n" +
			"func TestLoopback(t *testing.T) {\n\tl, err := net.Listen(\"tcp\", \"127.0.0.1:0\")\n" +
			"\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tgo func() { c, _ := l.Accept(); _ = c.Close() }()\n" +
			"\tc, err := net.Dial(\"tcp\", l.Addr().String())\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
			"\t_ = c.Close()\n\t_ = l.Close()\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	payload := testPayload(workspace, "GOTOOLCHAIN=local go test ./...")
	result, err := exec.Execute(context.Background(), payload)
	if err != nil || !result.Success {
		t.Fatalf("go test in the jail: err = %v, success = %v\nstdout = %s\nstderr = %s",
			err, result.Success, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "ok") {
		t.Fatalf("stdout = %q, want go test's ok line", result.Stdout)
	}
}

// hostGoroot is the running Go toolchain's root, or a skip when go is absent.
func hostGoroot(t *testing.T) string {
	t.Helper()
	out, err := osexec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skipf("go toolchain unavailable: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func acceptAndClose(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}
