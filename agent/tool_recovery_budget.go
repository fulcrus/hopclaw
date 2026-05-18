package agent

func (a *AgentComponent) toolRecoveryBudget(run *Run) int {
	base := 3
	if a != nil && a.config.MaxToolRecoveryAttempts > 0 {
		base = a.config.MaxToolRecoveryAttempts
	}
	return base + buildRunHarnessSpec(run, nil, "", nil).Recovery.ExtraAttempts
}
