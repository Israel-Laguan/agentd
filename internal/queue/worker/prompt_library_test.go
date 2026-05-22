package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestPromptLibrary_Render_AllSlots(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]string{
		slotLanguage:  "go",
		slotFilename:  "foo.go",
		slotSignature: "func Foo() int",
		slotTestCases: "- Foo() returns 1",
	}
	rendered, err := lib.Render("CODE_PROMPT_BUILDER", RenderSession{}, slots)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.System, "raw source code") {
		t.Fatalf("system missing code-gen instruction: %q", rendered.System)
	}
	if !strings.Contains(rendered.User, "go") || !strings.Contains(rendered.User, "foo.go") {
		t.Fatalf("user missing substituted slots: %q", rendered.User)
	}
	if !strings.Contains(rendered.User, "func Foo() int") {
		t.Fatalf("user missing signature: %q", rendered.User)
	}
}

func TestPromptLibrary_Render_MissingSlot(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.Render("CODE_PROMPT_BUILDER", RenderSession{}, map[string]string{
		slotLanguage: "go",
		slotFilename: "foo.go",
	})
	if err == nil {
		t.Fatal("expected error for missing slot")
	}
	if !strings.Contains(err.Error(), "CODE_PROMPT_BUILDER") || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want template name and missing slot", err)
	}
}

func TestPromptLibrary_Render_SystemPrefixPrepended(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]string{
		slotLanguage:  "python",
		slotFilename:  "main.py",
		slotSignature: "def main(): pass",
		slotTestCases: "None",
	}
	rendered, err := lib.Render("CODE_PROMPT_BUILDER", RenderSession{SystemPrefix: "GLOBAL RULES"}, slots)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rendered.System, "GLOBAL RULES") {
		t.Fatalf("system should start with prefix, got %q", rendered.System)
	}
	if !strings.Contains(rendered.System, "raw source code") {
		t.Fatalf("system should include template body after prefix: %q", rendered.System)
	}
}

func TestPromptLibrary_CODE_PROMPT_BUILDER(t *testing.T) {
	lib, err := NewPromptLibrary("")
	if err != nil {
		t.Fatal(err)
	}
	slots := buildCodeGenSlots(models.Task{
		Title:       "Add parser",
		Description: "Implement parseToken in parser.go\nSignature:\nfunc parseToken(s string) (string, error)\nTest cases:\n- empty input errors",
	})
	rendered, err := lib.Render(TemplateCodePromptBuilder, RenderSession{SystemPrefix: "STACK"}, slots)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(rendered.System + rendered.User)
	for _, forbidden := range []string{"```", "markdown", "json"} {
		if strings.Contains(lower, forbidden) && strings.Contains(rendered.System, forbidden) {
			// system explicitly forbids markdown/json — allow "json" only if negated
			if forbidden == "json" && strings.Contains(rendered.System, "or JSON") {
				continue
			}
		}
	}
	if strings.Contains(rendered.System, "```") {
		t.Fatalf("system should not encourage fences: %q", rendered.System)
	}
	if !strings.Contains(rendered.User, "parser.go") {
		t.Fatalf("user should include filename: %q", rendered.User)
	}
	if !strings.Contains(rendered.User, "parseToken") {
		t.Fatalf("user should include signature: %q", rendered.User)
	}
}

func TestPromptLibrary_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")
	lib, err := NewPromptLibrary(path)
	if err != nil {
		t.Fatal(err)
	}
	custom := PromptTemplate{
		System:        "SYS {{x}}",
		UserPrompt:    "USER {{x}}",
		RequiredSlots: []string{"x"},
	}
	if err := lib.Save("CUSTOM", custom); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CUSTOM") {
		t.Fatalf("file missing saved template: %s", data)
	}
	rendered, err := lib.Render("CUSTOM", RenderSession{}, map[string]string{"x": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.System != "SYS ok" || rendered.User != "USER ok" {
		t.Fatalf("rendered = %#v", rendered)
	}
}

func TestBuildCodeGenSlots(t *testing.T) {
	slots := buildCodeGenSlots(models.Task{
		Title:       "Fix handler",
		Description: "Update internal/api/handler.go\nSignature:\nfunc Handle() error",
	})
	if slots[slotLanguage] != "go" {
		t.Fatalf("language = %q, want go", slots[slotLanguage])
	}
	if slots[slotFilename] != "handler.go" {
		t.Fatalf("filename = %q, want handler.go", slots[slotFilename])
	}
}
