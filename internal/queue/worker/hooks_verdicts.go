package worker

func appendPreVerdict(ctx HookContext, hookName, outcome string) {
	if ctx.Verdicts == nil {
		return
	}
	*ctx.Verdicts = append(*ctx.Verdicts, hookName+":"+outcome)
}

func appendPostVerdict(ctx HookContext, hookName, outcome string) {
	if ctx.Verdicts == nil {
		return
	}
	*ctx.Verdicts = append(*ctx.Verdicts, hookName+":"+outcome)
}

func preVerdictOutcome(v HookVerdict) string {
	switch {
	case v.ShortCircuit:
		return "short_circuit"
	case v.Suspend:
		return "suspend"
	case v.Veto:
		return "veto"
	default:
		return "pass"
	}
}
