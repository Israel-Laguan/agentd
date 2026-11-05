package worker

import (
	"os"
	"testing"

	"agentd/internal/gateway/route"
	"agentd/internal/sandbox"
)

func TestACPRuntimeIsWiredOnlyForAnACPAgentOnTheJail(t *testing.T) {
	t.Parallel()
	model := route.Table{Agents: map[string]route.Agent{"quick": {Kind: route.KindModel}}}
	if rt, err := NewACPRuntime(model, &sandbox.BashExecutor{}, "/opt/agentd", nil); rt != nil || err != nil {
		t.Fatalf("model agents only: runtime %v, %v; want none", rt, err)
	}
	acp := route.Table{Agents: map[string]route.Agent{"coder": {Kind: route.KindACP, Preset: "opencode"}}}
	if _, err := NewACPRuntime(acp, nil, "/opt/agentd", nil); err == nil {
		t.Fatal("an acp agent wired with no bash executor; it runs only in its jail")
	}
	rt, err := NewACPRuntime(acp, &sandbox.BashExecutor{Root: t.TempDir()}, "/opt/agentd", nil)
	if err != nil || rt.MCPURL != "http://"+sandbox.JailMCPAddr {
		t.Fatalf("runtime %+v, %v", rt, err)
	}
	run, err := rt.Start([]string{"model.test:443"}, "/opt/agent")
	if err != nil {
		t.Fatal(err)
	}
	run.Close()
	if _, err := os.Stat(run.Home); !os.IsNotExist(err) {
		t.Fatalf("run home survived Close: %v", err)
	}
}
