package richedit

// CompletionItem describes one editor completion candidate.
type CompletionItem struct {
	Kind   TokenKind
	Label  string
	Detail string
	Path   string
}

// CompletionProvider resolves @ candidates plus Tab-based path/slash completions.
type CompletionProvider interface {
	AttachmentCandidates(query string) []CompletionItem
	CompletePath(prefix string) (string, bool)
	CompleteSlash(linePrefix, currentArg string) (string, bool)
}
