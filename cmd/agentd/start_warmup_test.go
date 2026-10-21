package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// B-083: the daemon only listened after the LLM warmup answered, so /health
// and every API call got "connection refused" for as long as the provider
// took. The stub provider below holds the warmup call open until the test
// has seen /health answer.
func TestStart_HealthAnswersWhileWarmupRuns(t *testing.T) {
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockOpenAISuccessBody))
	}))
	defer provider.Close()
	defer closeOnce(release)

	home := t.TempDir()
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "test-key")
	runCommand(t, context.Background(), "--home", home, "init")
	addr := freeAddress(t)
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	config := fmt.Sprintf("gateway:\n  order:\n  - openai\n  openai:\n    base_url: %q\napi:\n  address: %q\n", provider.URL+"/v1", addr)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		cmd := newRootCommand()
		cmd.SetArgs([]string{"--home", home, "--config", configPath, "start"})
		done <- cmd.ExecuteContext(ctx)
	}()

	body := waitForHealth(t, "http://"+addr+"/health", 10*time.Second)
	if warming, _ := body["warming_up"].(bool); !warming {
		t.Errorf("/health during warmup = %v; want warming_up true", body)
	}
	closeOnce(release)
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("start did not return after cancel")
	}
}

func runCommand(t *testing.T, ctx context.Context, args ...string) {
	t.Helper()
	cmd := newRootCommand()
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatalf("agentd %v: %v", args, err)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// waitForHealth polls url until it answers 200 and returns the decoded body.
func waitForHealth(t *testing.T, url string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx // short-lived test poll
		if err == nil {
			var body map[string]any
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil {
				return body
			}
			err = fmt.Errorf("status %d, decode %v", resp.StatusCode, decodeErr)
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("/health never answered 200 within %s while the warmup was held: %v", timeout, lastErr)
	return nil
}

func closeOnce(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
