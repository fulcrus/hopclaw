//go:build darwin

package desktopd

import "strings"

const (
	commandRiskLow      = "low"
	commandRiskMedium   = "medium"
	commandRiskHigh     = "high"
	commandRiskCritical = "critical"

	commandSafetyInspect     = "inspect"
	commandSafetyNavigation  = "navigation"
	commandSafetyEdit        = "edit"
	commandSafetyMedia       = "media"
	commandSafetySystem      = "system"
	commandSafetyDestructive = "destructive"
)

type desktopCommandListOptions struct {
	IncludeSystem bool
	IncludeUnsafe bool
	MaxResults    int
}

func applyCommandPolicy(driver desktopAppDriver, command desktopCommandSnapshot) desktopCommandSnapshot {
	policy := classifyCommandPolicy(driver, command)
	command.Aliases = policy.aliases
	command.Source = policy.source
	command.Scope = policy.scope
	command.RiskLevel = policy.riskLevel
	command.SafetyClass = policy.safetyClass
	command.RequiresConfirmation = policy.requiresConfirmation
	command.AvailableByDefault = policy.availableByDefault
	command.DriverID = driver.ID
	command.SupportTier = strings.ToUpper(driver.SupportTier)
	return command
}

func filterCommandSnapshots(commands []desktopCommandSnapshot, options desktopCommandListOptions) []desktopCommandSnapshot {
	out := make([]desktopCommandSnapshot, 0, len(commands))
	for _, command := range commands {
		if !options.IncludeSystem && command.Source == "system" {
			continue
		}
		if !options.IncludeUnsafe && !command.AvailableByDefault {
			continue
		}
		out = append(out, command)
		if options.MaxResults > 0 && len(out) >= options.MaxResults {
			return out
		}
	}
	return out
}

func commandBlockedByPolicy(command desktopCommandSnapshot, allowUnsafe bool) error {
	if allowUnsafe {
		return nil
	}
	if command.AvailableByDefault {
		return nil
	}
	switch command.Source {
	case "system":
		return commandPolicyError(command, "blocked system command by default; retry with params.allow_unsafe=true only in expert mode")
	default:
		return commandPolicyError(command, "blocked unsafe command by default; retry with params.allow_unsafe=true if this action is intentional")
	}
}

func commandPolicyError(command desktopCommandSnapshot, message string) error {
	return &commandPolicyViolation{
		CommandID: command.CommandID,
		MenuPath:  command.MenuPathString,
		Message:   message,
	}
}

type commandPolicyViolation struct {
	CommandID string
	MenuPath  string
	Message   string
}

func (e *commandPolicyViolation) Error() string {
	label := strings.TrimSpace(e.MenuPath)
	if label == "" {
		label = strings.TrimSpace(e.CommandID)
	}
	return "command " + `"` + label + `"` + " " + strings.TrimSpace(e.Message)
}

type commandPolicy struct {
	source               string
	scope                string
	riskLevel            string
	safetyClass          string
	requiresConfirmation bool
	availableByDefault   bool
	aliases              []string
}

func classifyCommandPolicy(driver desktopAppDriver, command desktopCommandSnapshot) commandPolicy {
	policy := commandPolicy{
		source:             "menu",
		scope:              "app",
		riskLevel:          commandRiskMedium,
		safetyClass:        commandSafetyNavigation,
		availableByDefault: true,
	}

	topMenu := ""
	if len(command.MenuPath) > 0 {
		topMenu = command.MenuPath[0]
	}
	titleNorm := normalizeCommandText(command.Title)
	pathNorm := normalizeCommandText(command.MenuPathString)

	if isSystemMenu(topMenu) {
		policy.source = "system"
		policy.scope = "global"
		policy.safetyClass = commandSafetySystem
		policy.availableByDefault = false
		policy.riskLevel = systemCommandRisk(pathNorm)
		policy.requiresConfirmation = policy.riskLevel == commandRiskHigh || policy.riskLevel == commandRiskCritical
		return policy
	}

	switch {
	case containsAnyNormalized(pathNorm, "forcequit", "强制退出", "shutdown", "关机", "restart", "重新启动", "logout", "退出登录", "lockscreen", "锁定屏幕"):
		policy.riskLevel = commandRiskCritical
		policy.safetyClass = commandSafetyDestructive
		policy.availableByDefault = false
		policy.requiresConfirmation = true
	case containsAnyNormalized(pathNorm, "quit", "退出", "delete", "删除", "remove", "移除", "trash", "废纸篓", "clear", "清空", "reset", "重置", "erase", "抹掉", "empty", "关闭项目"):
		policy.riskLevel = commandRiskHigh
		policy.safetyClass = commandSafetyDestructive
		policy.availableByDefault = false
		policy.requiresConfirmation = true
	case containsAnyNormalized(pathNorm, "save", "保存", "export", "导出", "share", "共享"):
		policy.riskLevel = commandRiskMedium
		policy.safetyClass = commandSafetyEdit
	case containsAnyNormalized(pathNorm, "play", "pause", "播放", "暂停", "next", "上一", "下一", "上一首", "下一首"):
		policy.riskLevel = commandRiskLow
		policy.safetyClass = commandSafetyMedia
	case containsAnyNormalized(pathNorm, "about", "关于", "help", "帮助", "preferences", "设置", "settings", "search", "搜索", "find", "查找", "window", "窗口", "view", "显示"):
		policy.riskLevel = commandRiskLow
		policy.safetyClass = commandSafetyInspect
	case containsAnyNormalized(pathNorm, "copy", "paste", "cut", "undo", "redo", "selectall", "复制", "粘贴", "剪切", "撤销", "重做", "全选"):
		policy.riskLevel = commandRiskLow
		policy.safetyClass = commandSafetyEdit
	}

	policy.aliases = driverCommandAliases(driver, command, titleNorm, pathNorm)
	return policy
}

func isSystemMenu(title string) bool {
	title = strings.TrimSpace(title)
	switch title {
	case "Apple", "", "苹果":
		return true
	default:
		return false
	}
}

func systemCommandRisk(pathNorm string) string {
	switch {
	case containsAnyNormalized(pathNorm, "forcequit", "shutdown", "关机", "restart", "重新启动", "logout", "退出登录", "lockscreen", "锁定屏幕"):
		return commandRiskCritical
	case containsAnyNormalized(pathNorm, "sleep", "睡眠"):
		return commandRiskHigh
	default:
		return commandRiskMedium
	}
}

func driverCommandAliases(driver desktopAppDriver, command desktopCommandSnapshot, titleNorm, pathNorm string) []string {
	aliases := make([]string, 0, 3)
	switch driver.ID {
	case "qqmusic":
		switch {
		case containsAnyNormalized(titleNorm, "设置"):
			aliases = append(aliases, "preferences", "settings")
		case containsAnyNormalized(pathNorm, "编辑搜索", "editsearch"):
			aliases = append(aliases, "app.search.focus", "search.submit")
		case equalsAnyNormalized(titleNorm, "播放", "暂停", "play", "pause"):
			aliases = append(aliases, "media.play_toggle")
		}
	case "douyin":
		switch {
		case equalsAnyNormalized(titleNorm, "下一", "下一个", "next"):
			aliases = append(aliases, "media.next_item")
		}
	case "premiere_pro":
		switch {
		case containsAnyNormalized(pathNorm, "最近使用", "recent"):
			aliases = append(aliases, "project.open_recent")
		case equalsAnyNormalized(titleNorm, "播放", "暂停", "停止", "play", "pause", "toggleplayback"):
			aliases = append(aliases, "timeline.play_toggle")
		}
	}
	return aliases
}

func normalizeCommandText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		" ", "",
		">", "",
		"…", "",
		"...", "",
		"“", "",
		"”", "",
		"\"", "",
		"'", "",
		"‘", "",
		"’", "",
		"，", "",
		",", "",
		"：", "",
		":", "",
		"（", "",
		"）", "",
		"(", "",
		")", "",
	)
	return replacer.Replace(value)
}

func containsAnyNormalized(value string, patterns ...string) bool {
	value = normalizeCommandText(value)
	if value == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = normalizeCommandText(pattern)
		if pattern != "" && strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}

func equalsAnyNormalized(value string, patterns ...string) bool {
	value = normalizeCommandText(value)
	if value == "" {
		return false
	}
	for _, pattern := range patterns {
		if value == normalizeCommandText(pattern) && value != "" {
			return true
		}
	}
	return false
}
