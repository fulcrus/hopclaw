package agent

import "strings"

func buildExecutionEvidencePrompt(run *Run, userMessage string, tools []ToolDefinition) string {
	message := strings.TrimSpace(userMessage)
	if message == "" {
		return ""
	}

	toolSet := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		toolSet[name] = struct{}{}
	}

	domains := cloneToolDomainSet(detectStructuredEvidence(message))
	if run != nil && run.Preflight != nil {
		for _, domain := range normalizeSemanticDomains(run.Preflight.SuggestedDomains) {
			if domains == nil {
				domains = make(map[ToolDomain]bool)
			}
			domains[ToolDomain(domain)] = true
		}
	}
	for domain, enabled := range activatedDomainsFromRun(run) {
		if enabled {
			if domains == nil {
				domains = make(map[ToolDomain]bool)
			}
			domains[domain] = true
		}
	}
	hasBrowserTools := hasAnyTool(toolSet,
		"browser.open", "browser.navigate", "browser.click", "browser.fill", "browser.type",
		"browser.wait", "browser.snapshot", "browser.screenshot",
	)
	hasDesktopTools := hasAnyTool(toolSet,
		"desktop.list_apps", "desktop.list_windows", "desktop.screenshot",
		"desktop.get_clipboard", "desktop.set_clipboard", "desktop.focus_window", "desktop.type_text",
	)
	hasBrowser := hasBrowserTools && domains[DomainBrowser]
	hasDesktop := hasDesktopTools && domains[DomainDesktop]
	needsFileEvidence := hasExecutionEvidenceFileContext(message, domains)
	needsSearchResultsGuidance := hasBrowser && (domains[DomainSearch] || domains[DomainWeb])

	lines := []string{
		"## Evidence Rules",
		"",
		"- Never present guessed, example, or imagined tool/command output as if it actually happened in this run.",
		"- If you have not called a tool yet, do not claim page titles, page content, screenshots, file contents, command output, submission success, or test results.",
		"- Do not paste raw JSON, HTML, shell transcripts, or tool payloads as the final answer. Summarize the evidence in natural language instead.",
	}

	if hasBrowser {
		lines = append(lines,
			"- This request needs real browser evidence. Use `browser.*` tools instead of shell commands, `curl`, or `net.fetch` for page interaction tasks.",
			"- Do not guess local browser cache, temp files, or hidden session paths. If you need page content, use `browser.snapshot`, `browser.screenshot`, or other `browser.*` tools directly.",
			"- Use `browser.type` only for text inputs or textareas. Use `browser.select` for `<select>` elements, and use `browser.click` for checkboxes, radio buttons, and submit buttons.",
			"- If selectors are unstable, use `browser.snapshot_aria` to discover refs, then use `browser.type_aria` for text fields and `browser.click_aria` for buttons or submit controls.",
			"- For form-fill tasks, once `browser.snapshot_aria` already exposed the needed refs, avoid repeated ARIA snapshots unless the page changed. Fill the needed fields and submit in the shortest reliable sequence.",
			"- When `browser.snapshot_aria` lists several buttons, choose the ref whose accessible name matches the requested action, such as a visible submit/confirm label, instead of clicking the next button by ref order.",
			"- For generic form-fill tasks, prioritize plain text fields, required choices, and the submit control. Skip optional specialized widgets such as time pickers or spinbuttons unless the user explicitly asked for them or the page blocks submission without them.",
			"- When several independent field refs are already known, batch the corresponding `browser.type_aria` calls and the final submit click in the same tool round when it is safe to do so.",
			"- When the user asked to submit a form or press a button, do not stop after typing. Finish the interaction with `browser.click` or `browser.click_aria`, then verify the post-action page state.",
			"- Before reporting success on a browser task, leave concrete evidence such as a snapshot, screenshot, extracted fields, or the post-submit URL/title.",
			"- If the page is dynamic, wait for the required state before extracting or summarizing content.",
		)
		if needsSearchResultsGuidance {
			lines = append(lines,
				"- For search-results extraction tasks, wait until the results list is visibly loaded once, then extract the first requested items and finish the answer.",
				"- If the current session already has a loaded search-results page, use that current page as the target instead of asking the user to restate the query.",
				"- Prefer `browser.wait` with one page evidence pass such as `browser.snapshot` or `browser.screenshot` over repeated probing on a large search page.",
				"- If the titles or links are not obvious from the DOM snapshot, use `browser.snapshot_aria` once to read the visible result structure; do not keep alternating between DOM snapshots and ARIA snapshots on the same page.",
				"- Avoid `browser.element_text` or `browser.element_attr` on broad search-result containers unless you truly need one small targeted selector.",
				"- Avoid `browser.screenshot_labeled` on large search-result pages unless the user explicitly asked for labeled UI evidence.",
				"- Once the first requested results are captured, stop taking more snapshots and return the final answer.",
			)
		}
	}
	if hasDesktop {
		lines = append(lines,
			"- This request needs real desktop evidence. Use `desktop.*` tools instead of shell guesses or textual assumptions.",
			"- Before reporting success on a desktop task, leave concrete evidence such as a screenshot, window list, clipboard readback, or focused window title.",
		)
	}
	if needsFileEvidence {
		lines = append(lines,
			"- When the user asks about repository files, logs, or documents, inspect the real workspace first and reference the actual files you checked.",
			"- When writing a file, perform the write and mention the resulting path rather than describing an intended change.",
		)
	}
	if hasBrowser && needsFileEvidence {
		lines = append(lines,
			"- For browser-to-file tasks, gather page evidence with `browser.*` tools before reading unrelated workspace files or writing the destination file.",
			"- Use workspace file tools only for the explicit destination path, or for an existing output file you truly need to update.",
		)
	}

	if len(lines) <= 4 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func hasAnyTool(toolSet map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := toolSet[name]; ok {
			return true
		}
	}
	return false
}

func hasExecutionEvidenceFileContext(message string, domains map[ToolDomain]bool) bool {
	if domains[DomainFS] || domains[DomainDocument] || domains[DomainSheet] || domains[DomainPresentation] || domains[DomainPDF] {
		return true
	}
	return messageHasLocalPathReference(message)
}
