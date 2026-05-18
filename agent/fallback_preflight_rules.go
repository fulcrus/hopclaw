package agent

import "strings"

// These predicates intentionally avoid user-language phrase tables. Fallback
// preflight may only rely on technical tokens, conventional artifact names,
// and canonical HopClaw/browser markers when model semantic signals are absent.
var (
	fallbackPreflightWorkspaceReferenceSubjectPhrases = []string{
		"this file", "that file", "the file", "current file",
		"this document", "that document", "the document", "current document",
		"this spreadsheet", "that spreadsheet", "the spreadsheet", "current spreadsheet",
		"this presentation", "that presentation", "the presentation", "current presentation",
		"this repo", "that repo", "the repo",
		"this repository", "that repository", "the repository",
		"这个文件", "该文件", "当前文件",
		"这个文档", "该文档", "当前文档", "这份文档",
		"这个表格", "该表格", "当前表格", "这份表格",
		"这个演示文稿", "该演示文稿", "当前演示文稿",
		"这个幻灯片", "该幻灯片", "当前幻灯片",
		"这个仓库", "该仓库", "当前仓库",
		"现有文件", "已有文件", "现有文档", "已有文档",
		"现有表格", "已有表格", "现有演示文稿", "已有演示文稿",
		"现有幻灯片", "已有幻灯片", "现有仓库", "已有仓库",
	}
	fallbackPreflightWorkspaceReferenceSubjectTokens = []string{
		"artifact://",
		"dockerfile",
		"go.mod",
		"makefile",
		"package.json",
		"cargo.toml",
		"pom.xml",
		"readme.md",
	}
	fallbackPreflightWorkspaceMutatingPhrases = []string{
		"append", "edit", "extend", "fix", "improve", "merge", "modify", "overwrite",
		"polish", "proofread", "replace", "rewrite", "translate", "update",
		"追加", "编辑", "修改", "更新", "改写", "替换", "修复", "润色", "校对",
		"翻译", "续写", "扩写", "缩写", "补充", "合并", "覆盖",
	}
	fallbackPreflightWorkspaceMutationTokens = []string{
		"apply_patch",
		"fs.edit",
		"fs.write",
		"git.apply",
		"git.commit",
	}
	fallbackPreflightWorkspaceReadPhrases = []string{
		"analyze", "analyse", "audit", "diagnose", "inspect", "investigate", "read", "review",
		"scan", "search", "summarize", "summarise",
		"分析", "审阅", "检查", "查看", "读取", "读一下", "读一读", "检索", "搜索", "排查", "总结", "汇总",
	}
	fallbackPreflightWorkspaceReadTokens = []string{
		"fs.read",
		"git.diff",
		"git.show",
	}
	fallbackPreflightWorkspaceArtifactPhrases = []string{
		"file", "document", "spreadsheet", "worksheet", "presentation", "slides",
		"slide deck", "readme", "repo", "repository", "codebase",
		"文件", "文档", "表格", "工作表", "演示文稿", "幻灯片", "仓库", "代码库",
	}
	fallbackPreflightWorkspaceArtifactTokens = []string{
		"artifact://",
		"dockerfile",
		"go.mod",
		"makefile",
		"package.json",
		"cargo.toml",
		"pom.xml",
		"readme.md",
	}
	fallbackPreflightExistingSourceReviewPhrases = []string{"review", "summarize", "summarise", "审阅", "总结", "汇总"}
	fallbackPreflightExistingSourceReviewTokens  = []string{
		"fs.read",
		"git.diff",
		"git.show",
	}
	fallbackPreflightBrowserContextPhrases = []string{
		"current page", "opened page", "browser session", "current tab",
		"当前页面", "已打开页面", "浏览器页面", "当前标签页",
	}
	fallbackPreflightSearchResultsRequestPhrases = []string{
		"搜索结果", "search result", "search results",
		"wait for the search results", "wait until the results load",
	}
	fallbackPreflightFreshNavigationPhrases = []string{
		"打开页面，", "打开网页，", "open the page,", "open page,",
	}
	fallbackPreflightSearchResultsContextPhrases = []string{
		"search result", "search results", "search page", "serp",
		"搜索结果", "搜索页", "检索结果",
	}
	fallbackPreflightSearchEnginePhrases = []string{"search", "bing", "google", "duckduckgo", "baidu", "title="}
)

func fallbackPreflightMentionsWorkspaceReferenceSubject(lower string) bool {
	return containsAny(lower, fallbackPreflightWorkspaceReferenceSubjectTokens...) ||
		containsAny(lower, fallbackPreflightWorkspaceReferenceSubjectPhrases...)
}

func fallbackPreflightMentionsWorkspaceMutation(lower string) bool {
	return containsAny(lower, fallbackPreflightWorkspaceMutationTokens...) ||
		containsAny(lower, fallbackPreflightWorkspaceMutatingPhrases...)
}

func fallbackPreflightMentionsWorkspaceRead(lower string) bool {
	return containsAny(lower, fallbackPreflightWorkspaceReadTokens...) ||
		containsAny(lower, fallbackPreflightWorkspaceReadPhrases...)
}

func fallbackPreflightMentionsWorkspaceArtifact(lower string) bool {
	return containsAny(lower, fallbackPreflightWorkspaceArtifactTokens...) ||
		containsAny(lower, fallbackPreflightWorkspaceArtifactPhrases...)
}

func fallbackPreflightMentionsExistingSourceReview(lower string) bool {
	return containsAny(lower, fallbackPreflightExistingSourceReviewTokens...) ||
		containsAny(lower, fallbackPreflightExistingSourceReviewPhrases...)
}

func fallbackPreflightMentionsBrowserContext(lower string) bool {
	return hasBrowserReferenceSummaryPrefix(lower) ||
		strings.Contains(lower, "browser.") ||
		containsAny(lower, fallbackPreflightBrowserContextPhrases...)
}

func fallbackPreflightRequestsSearchResultsReuse(lower string) bool {
	return looksLikeSearchResultsExtractionRequest(lower) ||
		containsAny(lower, fallbackPreflightSearchResultsRequestPhrases...)
}

func fallbackPreflightRequestsFreshNavigation(lower string) bool {
	return len(explicitBrowserReferenceURLs(lower)) > 0 ||
		containsAny(lower, fallbackPreflightFreshNavigationPhrases...)
}

func fallbackPreflightMentionsSearchResultsContext(lower string) bool {
	return looksLikeSearchResultsExtractionRequest(lower) ||
		containsAny(lower, fallbackPreflightSearchResultsContextPhrases...)
}

func fallbackPreflightMentionsSearchEngine(lower string) bool {
	for _, rawURL := range explicitBrowserReferenceURLs(lower) {
		if browserURLLooksLikeSearchResults(rawURL) {
			return true
		}
	}
	return containsAny(lower, fallbackPreflightSearchEnginePhrases...)
}
