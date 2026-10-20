package routing

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
)

func mixRequest() spec.AIRequest {
	return spec.AIRequest{
		Purpose: spec.PurposeExecTurn,
		Messages: []spec.PromptMessage{
			{Role: "system", Content: "rules-rules"},
			{Role: "user", Content: "do the task"},
			{Role: "system", Content: "memory", Section: spec.SectionMemories},
			{Role: "assistant", Content: "calling"},
			{Role: "tool", Content: "file-bytes-here"},
			{Role: "tool", Content: "more", Section: spec.SectionFileContext},
			{Role: "user", Content: "please fix", Section: spec.SectionComments},
		},
	}
}

func TestRecordCarriesInputSizeBySection(t *testing.T) {
	p := &scriptedProvider{name: "p", replies: []scriptedReply{{content: "ok"}}}
	rec := &sliceRecorder{}
	if _, err := NewRouter(p).WithRecorder(rec).Generate(context.Background(), mixRequest()); err != nil {
		t.Fatal(err)
	}
	want := spec.Mix{
		spec.SectionSystem:      {Chars: 11, Messages: 1},
		spec.SectionTask:        {Chars: 11, Messages: 1},
		spec.SectionMemories:    {Chars: 6, Messages: 1},
		spec.SectionHistory:     {Chars: 7, Messages: 1},
		spec.SectionToolResults: {Chars: 15, Messages: 1},
		spec.SectionFileContext: {Chars: 4, Messages: 1},
		spec.SectionComments:    {Chars: 10, Messages: 1},
	}
	got := rec.recs[0].Mix
	for s, w := range want {
		if got[s] != w {
			t.Errorf("mix[%s] = %+v, want %+v", s, got[s], w)
		}
	}
	if got.Chars() != rec.recs[0].InputChars {
		t.Errorf("mix total %d != input chars %d", got.Chars(), rec.recs[0].InputChars)
	}
}

func TestMixOfNoMessagesIsEmpty(t *testing.T) {
	if m := spec.MixOf(nil); len(m) != 0 {
		t.Fatalf("mix of no messages = %v, want empty", m)
	}
}
