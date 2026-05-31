package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if strings.Contains(rendered.User, "```") {
		t.Fatalf("user should not contain fences: %q", rendered.User)
	}
	lowerUser := strings.ToLower(rendered.User)
	if strings.Contains(lowerUser, "markdown") {
		t.Fatalf("user should not request markdown: %q", rendered.User)
	}
	lowerSystem := strings.ToLower(rendered.System)
	if strings.Contains(lowerSystem, "json") && !strings.Contains(lowerSystem, "or json") {
		t.Fatalf("system should not contain json unless negated: %q", rendered.System)
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

func TestPromptLibrary_SaveConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")
	lib, err := NewPromptLibrary(path)
	if err != nil {
		t.Fatal(err)
	}
	tmplA := PromptTemplate{System: "A", UserPrompt: "a", RequiredSlots: []string{}}
	tmplB := PromptTemplate{System: "B", UserPrompt: "b", RequiredSlots: []string{}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- lib.Save("CONCURRENT_A", tmplA)
	}()
	go func() {
		defer wg.Done()
		errs <- lib.Save("CONCURRENT_B", tmplB)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "CONCURRENT_A") || !strings.Contains(body, "CONCURRENT_B") {
		t.Fatalf("file missing concurrent saves: %s", body)
	}
}

func TestBuildCodeGenSlots(t *testing.T) {
	slots := buildCodeGenSlots(models.Task{
		Title:       "Add parser",
		Description: "Implement parseToken in parser.go\nSignature:\nfunc parseToken(s string) (string, error)\nTest cases:\n- empty input errors",
	})
	if slots[slotLanguage] != "go" {
		t.Fatalf("language = %q, want go", slots[slotLanguage])
	}
	if slots[slotFilename] != "parser.go" {
		t.Fatalf("filename = %q, want parser.go", slots[slotFilename])
	}
	if strings.Contains(slots[slotSignature], "Test cases:") {
		t.Fatalf("signature should not include test cases section: %q", slots[slotSignature])
	}
	if !strings.Contains(slots[slotTestCases], "empty input") {
		t.Fatalf("test_cases missing body: %q", slots[slotTestCases])
	}
}

func TestExtractPromptSection(t *testing.T) {
	t.Parallel()
	text := "Implement parseToken in parser.go\nSignature:\nfunc parseToken(s string) (string, error)\nTest cases:\n- empty input errors"
	got := extractPromptSection(text, "Signature:", "fallback")
	if strings.Contains(got, "Test cases:") {
		t.Fatalf("signature should not include next section: %q", got)
	}
	if !strings.Contains(got, "parseToken") {
		t.Fatalf("signature missing body: %q", got)
	}
	if extractPromptSection(text, "Language:", "fallback") != "fallback" {
		t.Fatal("missing header should return fallback")
	}
}

func TestSubstituteSlots_LongerSlotNameFirst(t *testing.T) {
	t.Parallel()
	out := substituteSlots("{{foobar}} {{foo}}", map[string]string{
		"foo":    "A",
		"foobar": "B",
	})
	if out != "B A" {
		t.Fatalf("substituteSlots = %q, want %q", out, "B A")
	}
}
