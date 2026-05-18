//go:build darwin

package desktopd

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"

import (
	"os/exec"
	"runtime"
)

func darwinHostProfile() map[string]any {
	screenCapture := availabilityFromBool(bool(C.CGPreflightScreenCaptureAccess()))
	accessibility := availabilityFromBool(C.AXIsProcessTrusted() != 0)
	inputInjection := accessibility
	menuInventory := capabilityFromAvailability(accessibility)
	accessibilityTree := capabilityFromAvailability(accessibility)
	textInjection := capabilityFromAvailability(inputInjection)
	hotkey := capabilityFromAvailability(inputInjection)
	windowContent := capabilityFromAvailability(screenCapture)
	screenRecord := capabilityFromAvailability(screenCapture)

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
			"window_content": windowContent,
			"screen_record":  screenRecord,
		},
		"introspection": map[string]any{
			"process_inventory":   "yes",
			"window_inventory":    "yes",
			"menu_inventory":      menuInventory,
			"accessibility_tree":  accessibilityTree,
			"embedded_web_bridge": "no",
		},
		"execution": map[string]any{
			"hotkey":         hotkey,
			"text_injection": textInjection,
			"clipboard":      capabilityFromAvailability(true),
			"native_command": capabilityFromAvailability(commandAvailable("open") && commandAvailable("osascript")),
		},
		"environment_flags": map[string]any{
			"multi_display":       darwinActiveDisplayCount() > 1,
			"remote_session":      false,
			"high_dpi":            false,
			"headless":            false,
			"occlusion_detection": capabilityFromAvailability(true),
		},
	}
}

func darwinActiveDisplayCount() int {
	var count C.uint32_t
	C.CGGetActiveDisplayList(0, nil, &count)
	return int(count)
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
