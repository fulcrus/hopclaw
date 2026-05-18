package verify

func defaultVerifiers() []Verifier {
	return []Verifier{
		coreVerifier{},
		contractVerifier{},
		planCoverageVerifier{},
		toolExecutionVerifier{},
		browserVerifier{},
		desktopVerifier{},
		spreadsheetVerifier{},
		documentVerifier{},
		presentationVerifier{},
		emailVerifier{},
		watchNotificationVerifier{},
	}
}
