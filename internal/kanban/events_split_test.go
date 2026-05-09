package kanban

import "testing"

func TestSplitCommentPayload(t *testing.T) {
	a, b := splitCommentPayload("USER: hello there")
	if a != "USER" || b != "hello there" {
		t.Fatalf("colon split: %q %q", a, b)
	}
	a, b = splitCommentPayload("no-colon-here")
	if a != "" || b != "no-colon-here" {
		t.Fatalf("no colon: %q %q", a, b)
	}
}
