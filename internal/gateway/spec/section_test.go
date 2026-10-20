package spec

import "testing"

func TestLabelledTaskMessageMakesLaterUntaggedUserMessagesComments(t *testing.T) {
	mix := MixOf([]PromptMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "task", Section: SectionTask},
		{Role: "user", Content: "please fix"},
	})
	if got := mix[SectionComments]; got.Chars != 10 || got.Messages != 1 {
		t.Fatalf("comments = %+v, want the second user message (10 chars)", got)
	}
	if got := mix[SectionTask]; got.Chars != 4 || got.Messages != 1 {
		t.Fatalf("task = %+v, want only the labelled message", got)
	}
}
