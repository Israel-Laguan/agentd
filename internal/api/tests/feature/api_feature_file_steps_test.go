package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/api"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
)

// File handling step implementations

func (s *apiScenario) serverRunningWithFileStash(context.Context) error {
	return nil
}

func (s *apiScenario) sendOversizedChatMessage(context.Context) error {
	input := strings.Repeat("a", 80) + strings.Repeat("b", 80)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": input}},
	})
	s.resp = request(s.fileHandler(), http.MethodPost, "/v1/chat/completions", string(body))
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) classifierReceivedFileRef(context.Context) error {
	if !strings.Contains(s.gateway.lastClassifyIntent, "[agentd file reference]") {
		return fmt.Errorf("ClassifyIntent missing file reference: %q", s.gateway.lastClassifyIntent)
	}
	return nil
}

func (s *apiScenario) plannerReceivedTruncatedContent(context.Context) error {
	if !strings.Contains(s.gateway.lastPlanIntent, "[agentd file content]") {
		return fmt.Errorf("GeneratePlan missing file content block: %q", s.gateway.lastPlanIntent)
	}
	if strings.Contains(s.gateway.lastPlanIntent, strings.Repeat("b", 20)) {
		return fmt.Errorf("GeneratePlan received untruncated tail content")
	}
	return nil
}

func (s *apiScenario) sendChatWithFile(_ context.Context, name string) error {
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "Plan from the attached spec"}},
		"files": []map[string]string{{
			"name":    name,
			"content": strings.Repeat("first ", 20) + strings.Repeat("last ", 20),
		}},
	})
	s.resp = request(s.fileHandler(), http.MethodPost, "/v1/chat/completions", string(body))
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) classifierReceivedFileName(context.Context) error {
	if !strings.Contains(s.gateway.lastClassifyIntent, "name:") {
		return fmt.Errorf("ClassifyIntent missing file name reference: %q", s.gateway.lastClassifyIntent)
	}
	return nil
}

func (s *apiScenario) plannerReceivedFileContent(context.Context) error {
	if !strings.Contains(s.gateway.lastPlanIntent, "content:") {
		return fmt.Errorf("GeneratePlan missing read file content: %q", s.gateway.lastPlanIntent)
	}
	return nil
}

func (s *apiScenario) handler() http.Handler {
	summarizer := frontdesk.NewStatusSummarizer(s.store)
	return api.NewHandler(api.ServerDeps{Store: s.store, Gateway: s.gateway, Bus: s.bus, Summarizer: summarizer})
}

func (s *apiScenario) fileHandler() http.Handler {
	summarizer := frontdesk.NewStatusSummarizer(s.store)
	dir := s.fileStashDir
	if dir == "" {
		dir = "/tmp/agentd-test-stash"
	}
	stash := &frontdesk.FileStash{Dir: dir, StashThreshold: 20}
	return api.NewHandler(api.ServerDeps{
		Store: s.store, Gateway: s.gateway, Bus: s.bus, Summarizer: summarizer,
		FileStash: stash, Truncator: gateway.StrategyTruncator{Strategy: gateway.HeadTailStrategy{HeadRatio: 1}}, Budget: 40,
	})
}
