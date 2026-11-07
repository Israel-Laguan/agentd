package mcp

import "testing"

func TestParseAskHumanArgsTrimsAndRequiresQuestion(t *testing.T) {
	t.Parallel()
	args, err := ParseAskHumanArgs(`{"question":"  Which format?  ","options":["CSV","JSON"]}`)
	if err != nil {
		t.Fatalf("ParseAskHumanArgs: %v", err)
	}
	if args.Question != "Which format?" || len(args.Options) != 2 {
		t.Fatalf("args = %+v, want the trimmed question and both options", args)
	}
	args, err = ParseAskHumanArgs(`{"question":"Which format?","options":[" CSV ",""," ","JSON"]}`)
	if err != nil {
		t.Fatalf("ParseAskHumanArgs: %v", err)
	}
	if len(args.Options) != 2 || args.Options[0] != "CSV" || args.Options[1] != "JSON" {
		t.Fatalf("options = %q, want the trimmed non-blank options [CSV JSON]", args.Options)
	}
	for _, raw := range []string{`{}`, `{"question":"   "}`, `not json`} {
		if _, err := ParseAskHumanArgs(raw); err == nil {
			t.Fatalf("ParseAskHumanArgs(%s) error = nil, want an error", raw)
		}
	}
}
