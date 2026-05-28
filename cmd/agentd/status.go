package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/models"
)

func newStatusCommand(opts *rootOptions) *cobra.Command {
	var apiURL string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print queue status",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := resolveAPIBase(opts, apiURL)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			counts, err := fetchStatusCounts(cmd.Context(), base)
			if err != nil {
				return err
			}
			return printStatus(cmd, counts)
		},
	}
	cmd.Flags().StringVar(&apiURL, "api-url", "", "daemon API base URL (default: from config or http://127.0.0.1:8765)")
	return cmd
}

// resolveAPIBase returns the HTTP base URL for the daemon API.
// Priority: explicit --api-url flag > config file API.Address > hardcoded default.
func resolveAPIBase(opts *rootOptions, flagValue string) (string, error) {
	if flagValue != "" {
		return strings.TrimRight(flagValue, "/"), nil
	}
	cfg, err := config.Load(config.LoadOptions{
		HomeOverride: opts.home,
		ConfigFile:   opts.configFile,
	})
	if err != nil {
		return "", err
	}
	if cfg.API.Address != "" {
		addr := strings.TrimRight(cfg.API.Address, "/")
		if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			addr = "http://" + addr
		}
		return addr, nil
	}
	return "http://127.0.0.1:8765", nil
}

// fetchStatusCounts calls GET /api/v1/system/status and returns task state counts.
func fetchStatusCounts(ctx context.Context, base string) (map[models.TaskState]int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/system/status", nil)
	if err != nil {
		return nil, fmt.Errorf("build status request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		if isConnectionError(err) {
			return nil, errors.New("daemon is not running — start with 'agentd start'")
		}
		return nil, fmt.Errorf("status request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Status *struct {
				Summary struct {
					TasksByState map[string]int `json:"tasks_by_state"`
				} `json:"summary"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode status response: %w", err)
	}
	if envelope.Data.Status == nil {
		return nil, errors.New("no status data in daemon response")
	}

	counts := make(map[models.TaskState]int, len(envelope.Data.Status.Summary.TasksByState))
	for k, v := range envelope.Data.Status.Summary.TasksByState {
		counts[models.TaskState(k)] = v
	}
	return counts, nil
}

func isConnectionError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return isConnectionError(urlErr.Err)
	}
	return false
}

func printStatus(cmd *cobra.Command, counts map[models.TaskState]int) error {
	if err := writeLine(cmd.OutOrStdout(), "STATE             COUNT"); err != nil {
		return err
	}
	for _, state := range statusStates() {
		if err := writeFormat(cmd.OutOrStdout(), "%-17s %d\n", state, counts[state]); err != nil {
			return err
		}
	}
	return writeFormat(cmd.OutOrStdout(), "queue_length      %d\nactive_threads    %d\n",
		counts[models.TaskStateReady]+counts[models.TaskStateQueued], counts[models.TaskStateRunning])
}

func statusStates() []models.TaskState {
	return []models.TaskState{
		models.TaskStateReady, models.TaskStateQueued, models.TaskStateRunning,
		models.TaskStatePending, models.TaskStateCompleted, models.TaskStateFailed,
		models.TaskStateFailedRequiresHuman, models.TaskStateInConsideration,
	}
}
