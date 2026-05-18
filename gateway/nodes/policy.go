package nodes

// ---------------------------------------------------------------------------
// Node Command Policy — Platform-specific command allowlisting
// ---------------------------------------------------------------------------

// platformCommands defines which commands each platform supports.
var platformCommands = map[string][]string{
	"iOS": {
		"canvas.present", "canvas.hide", "canvas.snapshot",
		"camera.snap", "camera.clip", "camera.list",
		"location.get",
		"device.info", "device.status", "device.permissions",
		"contacts.search", "contacts.add",
		"calendar.events", "calendar.add",
		"reminders.add",
		"motion.activity", "motion.pedometer",
		"system.notify",
	},
	"Android": {
		"canvas.present", "canvas.hide", "canvas.snapshot",
		"camera.snap", "camera.clip", "camera.list",
		"location.get",
		"device.info", "device.status", "device.health",
		"contacts.search", "contacts.add",
		"calendar.events", "calendar.add",
		"reminders.add",
		"motion.activity", "motion.pedometer",
		"notifications.list",
	},
	"macOS": {
		"system.run", "browser.proxy",
		"desktop.proxy", "desktop.list_apps", "desktop.list_windows",
		"desktop.open_app", "desktop.focus_app", "desktop.focus_window",
		"desktop.type_text", "desktop.hotkey", "desktop.screenshot",
		"desktop.screen_record",
		"desktop.capture_tree", "desktop.clipboard_read", "desktop.clipboard_write",
		"desktop.mouse_move", "desktop.mouse_click", "desktop.scroll",
		"desktop.find_element", "desktop.find_text", "desktop.click_text",
		"screen.record", "camera.snap", "camera.list",
		"canvas.present", "canvas.hide", "canvas.snapshot",
		"location.get",
		"device.info", "device.status",
	},
	"Windows": {
		"system.run", "browser.proxy",
		"desktop.proxy", "desktop.list_apps", "desktop.open_app",
		"desktop.focus_window", "desktop.type_text", "desktop.hotkey",
		"desktop.screenshot", "desktop.clipboard_read", "desktop.clipboard_write",
		"desktop.mouse_move", "desktop.mouse_click",
		"desktop.find_element", "desktop.find_text", "desktop.click_text",
		"device.info", "device.status",
	},
	"Linux": {
		"system.run", "browser.proxy",
		"desktop.proxy", "desktop.list_apps", "desktop.open_app",
		"desktop.focus_window", "desktop.type_text", "desktop.hotkey",
		"desktop.screenshot", "desktop.clipboard_read", "desktop.clipboard_write",
		"desktop.mouse_move", "desktop.mouse_click",
		"desktop.find_element", "desktop.find_text", "desktop.click_text",
		"device.info", "device.status",
	},
}

// IsCommandAllowed checks whether a command is allowed on the given platform.
func IsCommandAllowed(platform, command string) bool {
	cmds, ok := platformCommands[platform]
	if !ok {
		return false
	}
	for _, c := range cmds {
		if c == command {
			return true
		}
	}
	return false
}

// PlatformCommands returns the list of allowed commands for a platform.
func PlatformCommands(platform string) []string {
	cmds, ok := platformCommands[platform]
	if !ok {
		return nil
	}
	result := make([]string, len(cmds))
	copy(result, cmds)
	return result
}
