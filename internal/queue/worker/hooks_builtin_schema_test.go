package worker

import (
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
)

func bashSchema() map[string]*gateway.FunctionParameters {
	return map[string]*gateway.FunctionParameters{
		"bash": {
			Type: "object",
			Properties: map[string]any{
				"command": map[string]any{"type": "string"},
			},
			Required: []string{"command"},
		},
	}
}

func schemaCtx(tool, args string) HookContext {
	return HookContext{ToolName: tool, Args: args, Timestamp: time.Now()}
}

func TestSchemaValidationHook_ValidArgs(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", `{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
}

func TestSchemaValidationHook_MissingRequired(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for missing required argument")
	}
	if !strings.Contains(verdict.Reason, "command") {
		t.Fatalf("expected reason to mention 'command', got %q", verdict.Reason)
	}
}

func TestSchemaValidationHook_UnknownArgument(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", `{"command":"echo hi","extra":"val"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for unknown argument")
	}
	if !strings.Contains(verdict.Reason, "extra") {
		t.Fatalf("expected reason to mention 'extra', got %q", verdict.Reason)
	}
}

func TestSchemaValidationHook_WrongType(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", `{"command":42}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for wrong type")
	}
	if !strings.Contains(verdict.Reason, "expected type") {
		t.Fatalf("expected reason to mention type, got %q", verdict.Reason)
	}
}

func TestSchemaValidationHook_InvalidJSON(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", `{broken`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for invalid JSON args")
	}
	if !strings.Contains(verdict.Reason, "not valid JSON") {
		t.Fatalf("expected reason to mention JSON, got %q", verdict.Reason)
	}
}

func TestSchemaValidationHook_UnknownTool_Passes(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("unknown_tool", `{"anything":"goes"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto for unknown tool: %s", verdict.Reason)
	}
}

func TestSchemaValidationHook_EmptyArgs_NoRequired(t *testing.T) {
	t.Parallel()
	registry := map[string]*gateway.FunctionParameters{
		"ping": {Type: "object", Properties: map[string]any{}},
	}
	hook := SchemaValidationHook(registry)
	verdict, err := hook.Fn(schemaCtx("ping", ``))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
}

func TestSchemaValidationHook_EmptyArgs_WithRequired(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(bashSchema())
	verdict, err := hook.Fn(schemaCtx("bash", ``))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for empty args with required fields")
	}
}

func TestSchemaValidationHook_TypeChecks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, propType, value string
		valid                 bool
	}{
		{"string ok", "string", `"hello"`, true},
		{"string bad", "string", `123`, false},
		{"number ok", "number", `3.14`, true},
		{"number bad", "number", `"nan"`, false},
		{"integer ok", "integer", `42`, true},
		{"integer bad float", "integer", `3.14`, false},
		{"boolean ok", "boolean", `true`, true},
		{"boolean bad", "boolean", `"yes"`, false},
		{"object ok", "object", `{"a":1}`, true},
		{"object bad", "object", `"nope"`, false},
		{"array ok", "array", `[1,2]`, true},
		{"array bad", "array", `"nope"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := map[string]*gateway.FunctionParameters{
				"tool": {
					Type:       "object",
					Properties: map[string]any{"arg": map[string]any{"type": tt.propType}},
				},
			}
			hook := SchemaValidationHook(registry)
			verdict, err := hook.Fn(schemaCtx("tool", `{"arg":`+tt.value+`}`))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict.Veto == tt.valid {
				t.Fatalf("Veto = %v, want %v (reason: %s)", verdict.Veto, !tt.valid, verdict.Reason)
			}
		})
	}
}

func TestSchemaValidationHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := SchemaValidationHook(nil)
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed policy, got %v", hook.Policy)
	}
}

func TestSchemaValidationHook_IntegrationViaHookChain(t *testing.T) {
	t.Parallel()
	registry := map[string]*gateway.FunctionParameters{
		"write": {
			Type: "object",
			Properties: map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			Required: []string{"path", "content"},
		},
	}
	hc := NewHookChain()
	hc.RegisterPre(SchemaValidationHook(registry))

	verdict := hc.RunPre(schemaCtx("write", `{"path":"foo.txt"}`))
	if !verdict.Veto {
		t.Fatal("expected schema veto for missing 'content'")
	}
	if !strings.Contains(verdict.Reason, "content") {
		t.Fatalf("expected reason to mention 'content', got %q", verdict.Reason)
	}
}

func TestDenylistAndSchema_ChainedOrder(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(DenylistHook("/workspace"))
	hc.RegisterPre(SchemaValidationHook(bashSchema()))

	verdict := hc.RunPre(schemaCtx("bash", `{"command":"sudo rm -rf /"}`))
	if !verdict.Veto {
		t.Fatal("expected denylist to veto first")
	}
	if !strings.Contains(verdict.Reason, "dangerous pattern") {
		t.Fatalf("expected denylist reason, got %q", verdict.Reason)
	}
}
