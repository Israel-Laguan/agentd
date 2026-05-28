package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockStatusResponse builds the JSON envelope that /api/v1/system/status returns.
func mockStatusResponse(tasksByState map[string]int) []byte {
	body, _ := json.Marshal(map[string]any{
		"status": "success",
		"data": map[string]any{
			"status": map[string]any{
				"kind":    "status_report",
				"message": "ok",
				"summary": map[string]any{
					"total_projects": 1,
					"tasks_by_state": tasksByState,
				},
			},
		},
	})
	return body
}

func TestResolveAPIBase_FlagWins(t *testing.T) {
	const custom = "http://custom:9999"
	got, err := resolveAPIBase(&rootOptions{}, custom)
	if err != nil {
		t.Fatalf("resolveAPIBase() unexpected error: %v", err)
	}
	if got != custom {
		t.Fatalf("resolveAPIBase() = %q, want %q", got, custom)
	}
}

func TestResolveAPIBase_FlagAddressNormalization(t *testing.T) {
	got, err := resolveAPIBase(&rootOptions{}, "127.0.0.1:8765/")
	if err != nil {
		t.Fatalf("resolveAPIBase() unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:8765" {
		t.Fatalf("resolveAPIBase() = %q, want %q", got, "http://127.0.0.1:8765")
	}
}

func TestResolveAPIBase_ConfigAddressNormalization(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	writeConfigTestFile(t, configPath, []byte("api:\n  address: https://daemon.example:8765/\n"))

	got, err := resolveAPIBase(&rootOptions{home: home, configFile: configPath}, "")
	if err != nil {
		t.Fatalf("resolveAPIBase() error = %v", err)
	}
	if got != "https://daemon.example:8765" {
		t.Fatalf("resolveAPIBase() = %q, want %q", got, "https://daemon.example:8765")
	}
}

func TestResolveAPIBase_ConfigLoadError(t *testing.T) {
	_, err := resolveAPIBase(&rootOptions{
		home:       filepath.Join(t.TempDir(), ".agentd"),
		configFile: filepath.Join(t.TempDir(), "missing.yaml"),
	}, "")
	if err == nil {
		t.Fatal("resolveAPIBase() error = nil, want config load failure")
	}
}

func TestStatus_APIURLCustomBase(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		if r.URL.Path != "/api/v1/system/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mockStatusResponse(map[string]int{"ready": 1}))
	}))
	defer srv.Close()

	home := t.TempDir()
	runCLI(t, home, "status", "--api-url", srv.URL)
	if gotHost == "" {
		t.Fatal("mock server never received a request")
	}
}

// TestStatus_HTTPFetch verifies that the status command fetches task counts
// from the daemon API and formats them correctly.
func TestStatus_HTTPFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mockStatusResponse(map[string]int{
			"READY": 1, "QUEUED": 1, "RUNNING": 1, "COMPLETED": 1, "FAILED": 1,
		}))
	}))
	defer srv.Close()

	home := t.TempDir()
	output := runCLI(t, home, "status", "--api-url", srv.URL)
	for _, expect := range []string{
		"STATE             COUNT",
		"READY             1",
		"QUEUED            1",
		"RUNNING           1",
		"COMPLETED         1",
		"FAILED            1",
		"queue_length      2",
		"active_threads    1",
	} {
		if !strings.Contains(output, expect) {
			t.Errorf("output missing %q\nfull output:\n%s", expect, output)
		}
	}
}

// TestStatus_ReadOnlyHome verifies that status succeeds even when the agentd
// home directory is not writable (the common sandbox failure mode).
func TestStatus_ReadOnlyHome(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mockStatusResponse(map[string]int{"ready": 2}))
	}))
	defer srv.Close()

	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Skipf("cannot chmod (may be running as root): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	output := runCLI(t, home, "status", "--api-url", srv.URL)
	if !strings.Contains(output, "STATE") {
		t.Errorf("expected status output, got: %s", output)
	}
}

// TestStatus_ReadOnlyHomeNoAPIFlag verifies that a read-only home does not trigger
// the openRuntime writability preflight; without a running daemon we get a
// connection error instead of "directory not writable".
func TestStatus_ReadOnlyHomeNoAPIFlag(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Skipf("cannot chmod (may be running as root): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	cmd := newRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--home", home, "status"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when daemon not running, got nil")
	}
	if strings.Contains(err.Error(), "directory not writable") ||
		strings.Contains(err.Error(), "data directories") {
		t.Fatalf("got writability preflight error, want connection/daemon error: %v", err)
	}
	if !strings.Contains(err.Error(), "daemon is not running") {
		t.Errorf("expected 'daemon is not running' in error, got: %v", err)
	}
}

// TestStatus_ConnectionRefused verifies that a friendly error is returned
// when the daemon is not running.
func TestStatus_ConnectionRefused(t *testing.T) {
	home := t.TempDir()
	cmd := newRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--home", home, "status", "--api-url", "http://127.0.0.1:1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when daemon not running, got nil")
	}
	if !strings.Contains(err.Error(), "daemon is not running") {
		t.Errorf("expected 'daemon is not running' in error, got: %v", err)
	}
}
