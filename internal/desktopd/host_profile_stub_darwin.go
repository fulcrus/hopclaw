//go:build darwin && !cgo

package desktopd

import (
	"os/exec"
	"runtime"
)

// CGO-free release builds cannot query CoreGraphics or Accessibility
// permission state, so return a conservative host profile that keeps
// the shape stable for callers while marking privileged capabilities
// as unknown/unavailable.
func darwinHostProfile() map[string]any {
	screenCapture := "unknown"
	accessibility := "unknown"
	inputInjection := "unknown"

	return map[string]any{
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"window_system": "cocoa",
		"permissions": map[string]any{
			"screen_capture":  screenCapture,
			"accessibility":   accessibility,
			"input_injection": inputInjection,
			"app_scripting":   availabilityFromBool(commandAvailable("osascript")),
			"service_control": "unknown",
		},
		"capture": map[string]any{
			"full_screen":    capabilityFromAvailability(screenCapture),
			"window_content": capabilityFromAvailability(screenCapture),
			"screen_record":  capabilityFromAvailability(screenCapture),
		},
		"introspection": map[string]any{
			"process_inventory":   "yes",
			"window_inventory":    "yes",
			"menu_inventory":      capabilityFromAvailability(accessibility),
			"accessibility_tree":  capabilityFromAvailability(accessibility),
			"embedded_web_bridge": "no",
		},
		"execution": map[string]any{
			"hotkey":         capabilityFromAvailability(inputInjection),
			"text_injection": capabilityFromAvailability(inputInjection),
			"clipboard":      capabilityFromAvailability(true),
			"native_command": capabilityFromAvailability(commandAvailable("open") && commandAvailable("osascript")),
		},
		"environment_flags": map[string]any{
			"multi_display":       false,
			"remote_session":      false,
			"high_dpi":            false,
			"headless":            false,
			"occlusion_detection": capabilityFromAvailability(true),
		},
	}
}

func availabilityFromBool(ok bool) string {
	if ok {
		return "available"
	}
	return "denied"
}

func capabilityFromAvailability(value any) string {
	if text, ok := value.(string); ok {
		if text == "available" {
			return "yes"
		}
		return "no"
	}
	if flag, ok := value.(bool); ok && flag {
		return "yes"
	}
	return "no"
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
