package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

func warmupLLMIfNeeded(ctx context.Context, gw gateway.AIGateway, gcfg config.GatewayConfig, skipFlag, configEnabled bool) error {
	if skipFlag || !configEnabled {
		slog.Debug("LLM warmup skipped", "flag", skipFlag, "config_enabled", configEnabled)
		return nil
	}
	slog.Debug("running LLM warmup")
	if err := config.WarmupLLM(ctx, gw, gcfg); err != nil {
		return fmt.Errorf("LLM warmup: %w", err)
	}
	return nil
}

func startAPIServer(ctx context.Context, listener net.Listener, apiServer *http.Server, stop context.CancelFunc) <-chan error {
	errCh := make(chan error, 1)
	go func() { errCh <- apiServer.Serve(listener) }()
	go func() {
		<-ctx.Done()
		_ = apiServer.Shutdown(ctx)
	}()
	apiErrCh := make(chan error, 1)
	go func() {
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			apiErrCh <- err
			stop()
			return
		}
		close(apiErrCh)
	}()
	return apiErrCh
}

func drainAPIServerError(apiErrCh <-chan error) error {
	select {
	case err := <-apiErrCh:
		if err != nil {
			return fmt.Errorf("api server failed: %w", err)
		}
	default:
	}
	return nil
}
