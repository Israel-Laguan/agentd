package models

import "testing"

func TestSplitCommentPayload(t *testing.T) {
	t.Parallel()
	a, b := SplitCommentPayload("USER: hello there")
	if a != "USER" || b != "hello there" {
		t.Fatalf("colon split: %q %q", a, b)
	}
	a, b = SplitCommentPayload("no-colon-here")
	if a != "" || b != "no-colon-here" {
		t.Fatalf("no colon: %q %q", a, b)
	}
	a, b = SplitCommentPayload("WORKER_AGENT: agentd:hitl:draft-review\nclean output\n")
	if a != "WORKER_AGENT" || b != "agentd:hitl:draft-review\nclean output" {
		t.Fatalf("draft review trims trailing newline: author=%q body=%q", a, b)
	}
}
