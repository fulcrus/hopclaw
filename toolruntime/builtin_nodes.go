package toolruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

const (
	// nodesExecTimeout is the default timeout for external commands in nodes tools.
	nodesExecTimeout = 10 * time.Second

	// nodesDefaultDiskPath is the default path for disk usage checks.
	nodesDefaultDiskPath = "/"

	// bytesPerMB converts bytes to megabytes.
	bytesPerMB = 1024 * 1024

	// nodesLocationIPFallbackURL is the IP geolocation API endpoint.
	nodesLocationIPFallbackURL = "http://ip-api.com/json/?fields=lat,lon,query,status"

	// nodesScreenRecordMaxDuration is the maximum screen recording duration in seconds.
	nodesScreenRecordMaxDuration = 300

	// nodesScreenRecordTimeoutBuffer adds buffer on top of the recording duration for process startup.
	nodesScreenRecordTimeoutBuffer = 10 * time.Second

	// nodesScreenRecordDefaultFPS is the default frames-per-second for ffmpeg recordings.
	nodesScreenRecordDefaultFPS = 30

	// nodesScreenRecordDefaultResolution is the default capture resolution for linux.
	nodesScreenRecordDefaultResolution = "1920x1080"

	// nodesScreenRecordMaxFPS is the maximum allowed frames-per-second.
	nodesScreenRecordMaxFPS = 60

	// nodesProcessListMaxFilter limits how long the filter string can be.
	nodesProcessListMaxFilter = 256

	// nodesCameraExecTimeout is the timeout for camera capture commands.
	nodesCameraExecTimeout = 15 * time.Second
)

func nodesToolDefs(cfg BuiltinsConfig) []builtinToolDef {
	_ = cfg
	return []builtinToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.system_info",
				Description:     "Retrieve system information: OS, arch, hostname, CPUs, memory, Go version.",
				InputSchema:     nodesSystemInfoInputSchema(),
				OutputSchema:    nodesSystemInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:system_info",
			},
			Handler: handleNodesSystemInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.disk_usage",
				Description:     "Report disk usage for a given path.",
				InputSchema:     nodesDiskUsageInputSchema(),
				OutputSchema:    nodesDiskUsageOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:disk_usage",
			},
			Handler: handleNodesDiskUsage,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.notification",
				Description:     "Send a desktop notification.",
				InputSchema:     nodesNotificationInputSchema(),
				OutputSchema:    nodesNotificationOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:notification",
			},
			Handler: handleNodesNotification,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.open",
				Description:     "Open a URL or file path with the default application.",
				InputSchema:     nodesOpenInputSchema(),
				OutputSchema:    nodesOpenOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:open:{target}",
			},
			Handler: handleNodesOpen,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.network_info",
				Description:     "List network interfaces with their addresses and MAC.",
				InputSchema:     nodesNetworkInfoInputSchema(),
				OutputSchema:    nodesNetworkInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:network_info",
			},
			Handler: handleNodesNetworkInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.screen_capture",
				Description:     "Capture a screenshot and save it to disk.",
				InputSchema:     nodesScreenCaptureInputSchema(),
				OutputSchema:    nodesScreenCaptureOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:screen_capture",
			},
			Handler: handleNodesScreenCapture,
		},
		// -----------------------------------------------------------
		// New tools
		// -----------------------------------------------------------
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.camera_capture",
				Description:     "Capture a photo from the webcam and save it to disk.",
				InputSchema:     nodesCameraCaptureInputSchema(),
				OutputSchema:    nodesCameraCaptureOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:camera_capture",
			},
			Handler: handleNodesCameraCapture,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.camera_list",
				Description:     "List available camera/webcam devices.",
				InputSchema:     nodesCameraListInputSchema(),
				OutputSchema:    nodesCameraListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:camera_list",
			},
			Handler: handleNodesCameraList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.location",
				Description:     "Get approximate device location via GPS, network, or IP-based geolocation.",
				InputSchema:     nodesLocationInputSchema(),
				OutputSchema:    nodesLocationOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:location",
			},
			Handler: handleNodesLocation,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.clipboard_read",
				Description:     "Read the current contents of the system clipboard.",
				InputSchema:     nodesClipboardReadInputSchema(),
				OutputSchema:    nodesClipboardReadOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:clipboard_read",
			},
			Handler: handleNodesClipboardRead,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.clipboard_write",
				Description:     "Write text content to the system clipboard.",
				InputSchema:     nodesClipboardWriteInputSchema(),
				OutputSchema:    nodesClipboardWriteOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:clipboard_write",
			},
			Handler: handleNodesClipboardWrite,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.process_list",
				Description:     "List running OS processes with optional name filter.",
				InputSchema:     nodesProcessListInputSchema(),
				OutputSchema:    nodesProcessListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:process_list",
			},
			Handler: handleNodesProcessList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.env_info",
				Description:     "Get comprehensive environment information: user, home, shell, locale, paths.",
				InputSchema:     nodesEnvInfoInputSchema(),
				OutputSchema:    nodesEnvInfoOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:env_info",
			},
			Handler: handleNodesEnvInfo,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.screen_record",
				Description:     "Record the screen for a given duration and save to a video file.",
				InputSchema:     nodesScreenRecordInputSchema(),
				OutputSchema:    nodesScreenRecordOutputSchema(),
				SideEffectClass: "local_write",
				ExecutionKey:    "nodes:screen_record",
			},
			Handler: handleNodesScreenRecord,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "nodes.battery_status",
				Description:     "Get battery status including charge level, charging state, and power source.",
				InputSchema:     nodesBatteryStatusInputSchema(),
				OutputSchema:    nodesBatteryStatusOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "nodes:battery_status",
			},
			Handler: handleNodesBatteryStatus,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func nodesSystemInfoInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func nodesDiskUsageInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Filesystem path to check. Defaults to /.",
			},
		},
		"additionalProperties": false,
	}
}

func nodesNotificationInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Notification title.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Notification body text.",
			},
			"sound": map[string]any{
				"type":        "boolean",
				"description": "Whether to play a notification sound. Defaults to false.",
			},
		},
		"required":             []string{"title", "message"},
		"additionalProperties": false,
	}
}

func nodesOpenInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "URL or file path to open with the default application.",
			},
		},
		"required":             []string{"target"},
		"additionalProperties": false,
	}
}

func nodesNetworkInfoInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func nodesScreenCaptureInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output_path": map[string]any{
				"type":        "string",
				"description": "File path where the screenshot will be saved.",
			},
		},
		"required":             []string{"output_path"},
		"additionalProperties": false,
	}
}

func nodesCameraCaptureInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output_path": map[string]any{
				"type":        "string",
				"description": "File path where the captured photo will be saved.",
			},
			"device": map[string]any{
				"type":        "string",
				"description": "Camera device name or index. Defaults to the first available camera.",
			},
		},
		"required":             []string{"output_path"},
		"additionalProperties": false,
	}
}

func nodesCameraListInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func nodesLocationInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Maximum seconds to wait for a location fix. Defaults to 10.",
			},
		},
		"additionalProperties": false,
	}
}

func nodesClipboardReadInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func nodesClipboardWriteInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "Text content to write to the clipboard.",
			},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	}
}

func nodesProcessListInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filter": map[string]any{
				"type":        "string",
				"description": "Optional substring to filter process names.",
			},
		},
		"additionalProperties": false,
	}
}

func nodesEnvInfoInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func nodesScreenRecordInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output_path": map[string]any{
				"type":        "string",
				"description": "File path where the recording will be saved.",
			},
			"duration_sec": map[string]any{
				"type":        "integer",
				"description": "Recording duration in seconds. Maximum 300.",
			},
			"audio": map[string]any{
				"type":        "boolean",
				"description": "Whether to capture audio. Defaults to false.",
			},
			"fps": map[string]any{
				"type":        "integer",
				"description": "Frames per second, default 30. Only used on linux/windows (ffmpeg).",
			},
			"resolution": map[string]any{
				"type":        "string",
				"description": "Capture resolution like \"1920x1080\". Only used on linux (ffmpeg x11grab).",
			},
			"display": map[string]any{
				"type":        "string",
				"description": "Display identifier. Linux: \":0.0\", \":1.0\", etc. macOS: display index via screencapture -D flag.",
			},
			"quality": map[string]any{
				"type":        "string",
				"description": "Output quality preset: \"low\", \"medium\", \"high\". Affects ffmpeg encoding preset and CRF. Default \"medium\".",
			},
		},
		"required":             []string{"output_path", "duration_sec"},
		"additionalProperties": false,
	}
}

func nodesBatteryStatusInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func nodesSystemInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"os":              stringSchema("Operating system."),
		"arch":            stringSchema("CPU architecture."),
		"hostname":        stringSchema("Machine hostname."),
		"cpus":            integerSchema("Number of logical CPUs."),
		"memory_total_mb": integerSchema("Total memory in megabytes."),
		"memory_free_mb":  integerSchema("Estimated free memory in megabytes."),
		"go_version":      stringSchema("Go runtime version."),
	}, "os", "arch", "hostname", "cpus", "go_version")
}

func nodesDiskUsageOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":         stringSchema("Filesystem path checked."),
		"total_bytes":  integerSchema("Total disk space in bytes."),
		"free_bytes":   integerSchema("Free disk space in bytes."),
		"used_bytes":   integerSchema("Used disk space in bytes."),
		"used_percent": numberSchema("Percentage of disk space used."),
	}, "path", "total_bytes", "free_bytes", "used_bytes", "used_percent")
}

func nodesNotificationOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":       booleanSchema("Whether the notification was sent."),
		"platform": stringSchema("Platform on which the notification was sent."),
	}, "ok", "platform")
}

func nodesOpenOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":     booleanSchema("Whether the target was opened successfully."),
		"target": stringSchema("The target that was opened."),
	}, "ok", "target")
}

func nodesNetworkInfoOutputSchema() map[string]any {
	ifaceSchema := objectSchema(map[string]any{
		"name":      stringSchema("Interface name."),
		"addresses": arraySchema(stringSchema(""), "IP addresses assigned to the interface."),
		"mac":       stringSchema("Hardware MAC address."),
	}, "name")
	return objectSchema(map[string]any{
		"interfaces": arraySchema(ifaceSchema, "Network interfaces."),
	}, "interfaces")
}

func nodesScreenCaptureOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the screen capture succeeded."),
		"path":       stringSchema("Path where the screenshot was saved."),
		"size_bytes": integerSchema("File size in bytes."),
	}, "ok")
}

func nodesCameraCaptureOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":         booleanSchema("Whether the camera capture succeeded."),
		"path":       stringSchema("Path where the photo was saved."),
		"size_bytes": integerSchema("File size in bytes."),
	}, "ok")
}

func nodesCameraListOutputSchema() map[string]any {
	deviceSchema := objectSchema(map[string]any{
		"name":  stringSchema("Camera device name."),
		"index": integerSchema("Camera device index."),
	}, "name")
	return objectSchema(map[string]any{
		"cameras": arraySchema(deviceSchema, "Available camera devices."),
		"count":   integerSchema("Number of cameras found."),
	}, "cameras", "count")
}

func nodesLocationOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"latitude":        numberSchema("Latitude in decimal degrees."),
		"longitude":       numberSchema("Longitude in decimal degrees."),
		"accuracy_meters": numberSchema("Accuracy radius in meters."),
		"source":          stringSchema("Location source: gps, network, or ip."),
	}, "latitude", "longitude", "source")
}

func nodesClipboardReadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"content": stringSchema("Current clipboard text content."),
		"format":  stringSchema("Content format (text)."),
	}, "content", "format")
}

func nodesClipboardWriteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok": booleanSchema("Whether the clipboard write succeeded."),
	}, "ok")
}

func nodesProcessListOutputSchema() map[string]any {
	procSchema := objectSchema(map[string]any{
		"pid":         integerSchema("Process ID."),
		"name":        stringSchema("Process name."),
		"cpu_percent": numberSchema("CPU usage percentage."),
		"memory_mb":   numberSchema("Memory usage in megabytes."),
		"status":      stringSchema("Process status."),
	}, "pid", "name")
	return objectSchema(map[string]any{
		"processes": arraySchema(procSchema, "Running processes."),
		"count":     integerSchema("Number of processes returned."),
	}, "processes", "count")
}

func nodesEnvInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"user":            stringSchema("Current username."),
		"home_dir":        stringSchema("User home directory."),
		"shell":           stringSchema("Default shell."),
		"term":            stringSchema("Terminal type."),
		"lang":            stringSchema("Language/locale setting."),
		"path_dirs_count": integerSchema("Number of directories in PATH."),
		"temp_dir":        stringSchema("System temporary directory."),
		"working_dir":     stringSchema("Current working directory."),
	}, "user", "home_dir", "temp_dir", "working_dir")
}

func nodesScreenRecordOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"ok":           booleanSchema("Whether the screen recording succeeded."),
		"path":         stringSchema("Path where the recording was saved."),
		"size_bytes":   integerSchema("File size in bytes."),
		"duration_sec": integerSchema("Actual recording duration in seconds."),
		"fps":          integerSchema("Frames per second used for recording."),
		"quality":      stringSchema("Quality preset used: low, medium, or high."),
	}, "ok")
}

func nodesBatteryStatusOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"percent":            integerSchema("Battery charge percentage."),
		"charging":           booleanSchema("Whether the battery is currently charging."),
		"time_remaining_min": integerSchema("Estimated minutes remaining. -1 if unknown."),
		"power_source":       stringSchema("Power source: battery, ac, or unknown."),
	}, "percent", "charging", "power_source")
}

// ---------------------------------------------------------------------------
// Handlers — existing tools
// ---------------------------------------------------------------------------

func handleNodesSystemInfo(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	hostname, _ := os.Hostname()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	totalMB := int64(memStats.Sys) / int64(bytesPerMB)
	freeMB := int64(memStats.Sys-memStats.Alloc) / int64(bytesPerMB)

	return b.jsonResult(call, map[string]any{
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"hostname":        hostname,
		"cpus":            runtime.NumCPU(),
		"memory_total_mb": totalMB,
		"memory_free_mb":  freeMB,
		"go_version":      runtime.Version(),
	})
}

func handleNodesDiskUsage(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, _ := stringFrom(call.Input["path"])
	if strings.TrimSpace(path) == "" {
		path = nodesDefaultDiskPath
	}

	totalBytes, _, availableBytes, err := diskUsage(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.disk_usage: %w", err)
	}
	freeBytes := availableBytes
	usedBytes := totalBytes - availableBytes

	var usedPercent float64
	if totalBytes > 0 {
		usedPercent = float64(usedBytes) / float64(totalBytes) * 100.0
	}

	return b.jsonResult(call, map[string]any{
		"path":         path,
		"total_bytes":  totalBytes,
		"free_bytes":   freeBytes,
		"used_bytes":   usedBytes,
		"used_percent": usedPercent,
	})
}

func handleNodesNotification(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	title, err := requiredString(call.Input, "title")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.notification: %w", err)
	}
	message, err := requiredString(call.Input, "message")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.notification: %w", err)
	}
	sound, _ := boolFrom(call.Input["sound"])

	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	platform := runtime.GOOS
	var cmd *exec.Cmd

	switch platform {
	case "darwin":
		script := fmt.Sprintf("display notification %q with title %q", message, title)
		if sound {
			script += " sound name \"default\""
		}
		cmd = exec.CommandContext(execCtx, "osascript", "-e", script)

	case "linux":
		args := []string{title, message}
		cmd = exec.CommandContext(execCtx, "notify-send", args...)

	case "windows":
		psScript := fmt.Sprintf(
			`[System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms'); `+
				`$n=New-Object System.Windows.Forms.NotifyIcon; `+
				`$n.Icon=[System.Drawing.SystemIcons]::Information; `+
				`$n.Visible=$true; `+
				`$n.ShowBalloonTip(5000, '%s', '%s', 'Info')`,
			title, message,
		)
		cmd = exec.CommandContext(execCtx, "powershell", "-Command", psScript)

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.notification: unsupported platform %q", platform)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.notification: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":       true,
		"platform": platform,
	})
}

func handleNodesOpen(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	target, err := requiredString(call.Input, "target")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.open: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(execCtx, "open", target)
	case "linux":
		cmd = exec.CommandContext(execCtx, "xdg-open", target)
	case "windows":
		cmd = exec.CommandContext(execCtx, "cmd", "/c", "start", "", target)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.open: unsupported platform %q", runtime.GOOS)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.open: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":     true,
		"target": target,
	})
}

func handleNodesNetworkInfo(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.network_info: %w", err)
	}

	entries := make([]map[string]any, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		addrStrings := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addrStrings = append(addrStrings, addr.String())
		}

		entries = append(entries, map[string]any{
			"name":      iface.Name,
			"addresses": addrStrings,
			"mac":       iface.HardwareAddr.String(),
		})
	}

	return b.jsonResult(call, map[string]any{
		"interfaces": entries,
	})
}

func handleNodesScreenCapture(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_capture: %w", err)
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_capture: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		cmd = exec.CommandContext(execCtx, "screencapture", "-x", absPath)

	case "linux":
		// Try import (ImageMagick) first; fall back to gnome-screenshot.
		if _, lookErr := exec.LookPath("import"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "import", "-window", "root", absPath)
		} else if _, lookErr := exec.LookPath("gnome-screenshot"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "gnome-screenshot", "-f", absPath)
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_capture: no screenshot tool found (install imagemagick or gnome-screenshot)")
		}

	case "windows":
		psScript := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$screen = [System.Windows.Forms.Screen]::PrimaryScreen; ` +
			`$bitmap = New-Object System.Drawing.Bitmap($screen.Bounds.Width, $screen.Bounds.Height); ` +
			`$graphics = [System.Drawing.Graphics]::FromImage($bitmap); ` +
			`$graphics.CopyFromScreen($screen.Bounds.Location, [System.Drawing.Point]::Empty, $screen.Bounds.Size); ` +
			`$bitmap.Save('%s'); ` +
			`$graphics.Dispose(); ` +
			`$bitmap.Dispose()`
		cmd = exec.CommandContext(execCtx, "powershell", "-Command", fmt.Sprintf(psScript, absPath))

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_capture: unsupported platform %q", platform)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_capture: %w", err)
	}

	var sizeBytes int64
	if info, statErr := os.Stat(absPath); statErr == nil {
		sizeBytes = info.Size()
	}

	return b.jsonResult(call, map[string]any{
		"ok":         true,
		"path":       absPath,
		"size_bytes": sizeBytes,
	})
}

// ---------------------------------------------------------------------------
// Handlers — camera tools
// ---------------------------------------------------------------------------

func handleNodesCameraCapture(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: %w", err)
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: %w", err)
	}

	device, _ := stringFrom(call.Input["device"])

	execCtx, cancel := context.WithTimeout(ctx, nodesCameraExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		if _, lookErr := exec.LookPath("imagesnap"); lookErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: imagesnap not found (install via: brew install imagesnap)")
		}
		args := []string{absPath}
		if strings.TrimSpace(device) != "" {
			args = append([]string{"-d", device}, args...)
		}
		cmd = exec.CommandContext(execCtx, "imagesnap", args...)

	case "linux":
		if _, lookErr := exec.LookPath("fswebcam"); lookErr == nil {
			args := []string{"-r", "1280x720", "--no-banner", absPath}
			if strings.TrimSpace(device) != "" {
				args = append([]string{"-d", device}, args...)
			}
			cmd = exec.CommandContext(execCtx, "fswebcam", args...)
		} else if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			dev := "/dev/video0"
			if strings.TrimSpace(device) != "" {
				dev = device
			}
			cmd = exec.CommandContext(execCtx, "ffmpeg", "-f", "v4l2", "-i", dev,
				"-frames:v", "1", "-y", absPath)
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: no camera tool found (install fswebcam or ffmpeg)")
		}

	case "windows":
		dev := "0"
		if strings.TrimSpace(device) != "" {
			dev = device
		}
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "ffmpeg", "-f", "dshow",
				"-i", fmt.Sprintf("video=%s", dev), "-frames:v", "1", "-y", absPath)
		} else {
			// Fallback: PowerShell with DirectShow via .NET
			psScript := fmt.Sprintf(
				`Add-Type -AssemblyName System.Drawing; `+
					`Add-Type -AssemblyName System.Windows.Forms; `+
					`$ffmpeg = $null; `+
					`$deviceIndex = %s; `+
					`Write-Error 'nodes.camera_capture: ffmpeg is required on Windows (install ffmpeg and add to PATH)'`,
				dev,
			)
			cmd = exec.CommandContext(execCtx, "powershell", "-Command", psScript)
		}

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: unsupported platform %q", platform)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.camera_capture: %w", err)
	}

	var sizeBytes int64
	if info, statErr := os.Stat(absPath); statErr == nil {
		sizeBytes = info.Size()
	}

	return b.jsonResult(call, map[string]any{
		"ok":         true,
		"path":       absPath,
		"size_bytes": sizeBytes,
	})
}

func handleNodesCameraList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	cameras := make([]map[string]any, 0)
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		if _, lookErr := exec.LookPath("system_profiler"); lookErr == nil {
			cmd := exec.CommandContext(execCtx, "system_profiler", "SPCameraDataType")
			out, err := cmd.Output()
			if err == nil {
				cameras = parseDarwinCameraList(string(out))
			}
		}

	case "linux":
		// Enumerate /dev/video* devices.
		matches, err := filepath.Glob("/dev/video*")
		if err == nil {
			for i, m := range matches {
				name := filepath.Base(m)
				cameras = append(cameras, map[string]any{
					"name":  name,
					"index": i,
				})
			}
		}

	case "windows":
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
			cmd := exec.CommandContext(execCtx, "ffmpeg", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
			// ffmpeg writes device list to stderr.
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			cmd.Run() // Intentionally ignoring error; ffmpeg exits non-zero when listing devices.
			cameras = parseWindowsCameraList(stderr.String())
		}
	}

	return b.jsonResult(call, map[string]any{
		"cameras": cameras,
		"count":   len(cameras),
	})
}

// parseDarwinCameraList extracts camera names from system_profiler SPCameraDataType output.
func parseDarwinCameraList(output string) []map[string]any {
	cameras := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	idx := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Camera entries appear as lines ending with ":" that are not section headers.
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "Camera") &&
			line != "Cameras:" && len(line) > 1 {
			name := strings.TrimSuffix(line, ":")
			cameras = append(cameras, map[string]any{
				"name":  name,
				"index": idx,
			})
			idx++
		}
	}
	return cameras
}

// parseWindowsCameraList extracts camera names from ffmpeg -list_devices stderr output.
func parseWindowsCameraList(output string) []map[string]any {
	cameras := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	idx := 0
	inVideo := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "DirectShow video devices") {
			inVideo = true
			continue
		}
		if strings.Contains(line, "DirectShow audio devices") {
			inVideo = false
			continue
		}
		if inVideo && strings.Contains(line, "]  \"") {
			// Extract the device name between quotes.
			start := strings.Index(line, "\"")
			end := strings.LastIndex(line, "\"")
			if start >= 0 && end > start {
				name := line[start+1 : end]
				cameras = append(cameras, map[string]any{
					"name":  name,
					"index": idx,
				})
				idx++
			}
		}
	}
	return cameras
}

// ---------------------------------------------------------------------------
// Handlers — location tool
// ---------------------------------------------------------------------------

func handleNodesLocation(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	timeoutSec, err := intFrom(call.Input["timeout_sec"], 10)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.location: %w", err)
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	timeout := time.Duration(timeoutSec) * time.Second

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	platform := runtime.GOOS

	// Try platform-native location first, then fall back to IP-based.
	switch platform {
	case "darwin":
		result, nativeErr := nodesLocationDarwin(execCtx)
		if nativeErr == nil {
			return b.jsonResult(call, result)
		}
		// Fall through to IP-based.

	case "linux":
		result, nativeErr := nodesLocationLinux(execCtx)
		if nativeErr == nil {
			return b.jsonResult(call, result)
		}
		// Fall through to IP-based.

	case "windows":
		result, nativeErr := nodesLocationWindows(execCtx)
		if nativeErr == nil {
			return b.jsonResult(call, result)
		}
		// Fall through to IP-based.
	}

	// IP-based fallback — works on all platforms.
	result, err := nodesLocationIPFallback(execCtx)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.location: %w", err)
	}
	return b.jsonResult(call, result)
}

// nodesLocationDarwin attempts to get location on macOS using CoreLocationCLI or a swift snippet.
func nodesLocationDarwin(ctx context.Context) (map[string]any, error) {
	// Try CoreLocationCLI first (brew install corelocationcli).
	if _, lookErr := exec.LookPath("CoreLocationCLI"); lookErr == nil {
		cmd := exec.CommandContext(ctx, "CoreLocationCLI", "-once", "-format", "%latitude,%longitude,%accuracy")
		out, err := cmd.Output()
		if err == nil {
			parts := strings.SplitN(strings.TrimSpace(string(out)), ",", 3)
			if len(parts) >= 2 {
				lat, latErr := strconv.ParseFloat(parts[0], 64)
				lon, lonErr := strconv.ParseFloat(parts[1], 64)
				if latErr == nil && lonErr == nil {
					accuracy := 0.0
					if len(parts) >= 3 {
						accuracy, _ = strconv.ParseFloat(parts[2], 64)
					}
					return map[string]any{
						"latitude":        lat,
						"longitude":       lon,
						"accuracy_meters": accuracy,
						"source":          "gps",
					}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no native location provider available on darwin")
}

// nodesLocationLinux attempts to get location on Linux via the geoclue D-Bus API.
func nodesLocationLinux(ctx context.Context) (map[string]any, error) {
	// Try gdbus call to GeoClue2.
	if _, lookErr := exec.LookPath("gdbus"); lookErr == nil {
		cmd := exec.CommandContext(ctx, "gdbus", "call", "--system",
			"--dest", "org.freedesktop.GeoClue2",
			"--object-path", "/org/freedesktop/GeoClue2/Manager",
			"--method", "org.freedesktop.GeoClue2.Manager.GetClient")
		out, err := cmd.Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			// Parse the client path and query location. This is best-effort.
			// For simplicity, fall through to IP if GeoClue is not fully set up.
			_ = out
		}
	}
	return nil, fmt.Errorf("no native location provider available on linux")
}

// nodesLocationWindows attempts to get location on Windows via PowerShell.
func nodesLocationWindows(ctx context.Context) (map[string]any, error) {
	psScript := `Add-Type -AssemblyName System.Device; ` +
		`$loc = New-Object System.Device.Location.GeoCoordinateWatcher; ` +
		`$loc.Start(); ` +
		`Start-Sleep -Seconds 5; ` +
		`if ($loc.Status -eq 'Ready') { ` +
		`  $c = $loc.Position.Location; ` +
		`  Write-Output "$($c.Latitude),$($c.Longitude),$($c.HorizontalAccuracy)"; ` +
		`} else { Write-Error 'location not available' }; ` +
		`$loc.Stop()`
	cmd := exec.CommandContext(ctx, "powershell", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("windows location query failed: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), ",", 3)
	if len(parts) >= 2 {
		lat, latErr := strconv.ParseFloat(parts[0], 64)
		lon, lonErr := strconv.ParseFloat(parts[1], 64)
		if latErr == nil && lonErr == nil {
			accuracy := 0.0
			if len(parts) >= 3 {
				accuracy, _ = strconv.ParseFloat(parts[2], 64)
			}
			return map[string]any{
				"latitude":        lat,
				"longitude":       lon,
				"accuracy_meters": accuracy,
				"source":          "network",
			}, nil
		}
	}
	return nil, fmt.Errorf("failed to parse windows location output")
}

// nodesLocationIPFallback gets approximate location from an IP geolocation API.
func nodesLocationIPFallback(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodesLocationIPFallbackURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create IP location request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IP location request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read IP location response: %w", err)
	}

	var ipResult struct {
		Status string  `json:"status"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
	}
	if err := json.Unmarshal(body, &ipResult); err != nil {
		return nil, fmt.Errorf("failed to parse IP location response: %w", err)
	}
	if ipResult.Status != "success" {
		return nil, fmt.Errorf("IP location service returned status %q", ipResult.Status)
	}

	return map[string]any{
		"latitude":        ipResult.Lat,
		"longitude":       ipResult.Lon,
		"accuracy_meters": 0.0,
		"source":          "ip",
	}, nil
}

// ---------------------------------------------------------------------------
// Handlers — clipboard tools
// ---------------------------------------------------------------------------

func handleNodesClipboardRead(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		cmd = exec.CommandContext(execCtx, "pbpaste")

	case "linux":
		if _, lookErr := exec.LookPath("xclip"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "xclip", "-selection", "clipboard", "-o")
		} else if _, lookErr := exec.LookPath("xsel"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "xsel", "--clipboard", "--output")
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_read: no clipboard tool found (install xclip or xsel)")
		}

	case "windows":
		cmd = exec.CommandContext(execCtx, "powershell", "-Command", "Get-Clipboard")

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_read: unsupported platform %q", platform)
	}

	out, err := cmd.Output()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_read: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"content": string(out),
		"format":  "text",
	})
}

func handleNodesClipboardWrite(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	content, err := requiredString(call.Input, "content")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_write: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	var cmd *exec.Cmd
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		cmd = exec.CommandContext(execCtx, "pbcopy")
		cmd.Stdin = strings.NewReader(content)

	case "linux":
		if _, lookErr := exec.LookPath("xclip"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "xclip", "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(content)
		} else if _, lookErr := exec.LookPath("xsel"); lookErr == nil {
			cmd = exec.CommandContext(execCtx, "xsel", "--clipboard", "--input")
			cmd.Stdin = strings.NewReader(content)
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_write: no clipboard tool found (install xclip or xsel)")
		}

	case "windows":
		cmd = exec.CommandContext(execCtx, "powershell", "-Command",
			fmt.Sprintf("Set-Clipboard -Value '%s'", strings.ReplaceAll(content, "'", "''")))

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_write: unsupported platform %q", platform)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.clipboard_write: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok": true,
	})
}

// ---------------------------------------------------------------------------
// Handlers — process list tool
// ---------------------------------------------------------------------------

func handleNodesProcessList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	filter, _ := stringFrom(call.Input["filter"])
	filter = strings.TrimSpace(filter)
	if len(filter) > nodesProcessListMaxFilter {
		filter = filter[:nodesProcessListMaxFilter]
	}

	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	platform := runtime.GOOS
	var processes []map[string]any

	switch platform {
	case "darwin", "linux":
		cmd := exec.CommandContext(execCtx, "ps", "aux")
		out, err := cmd.Output()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.process_list: %w", err)
		}
		processes = parsePSAuxOutput(string(out), filter)

	case "windows":
		cmd := exec.CommandContext(execCtx, "tasklist", "/fo", "csv", "/nh")
		out, err := cmd.Output()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.process_list: %w", err)
		}
		processes = parseTasklistOutput(string(out), filter)

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.process_list: unsupported platform %q", platform)
	}

	return b.jsonResult(call, map[string]any{
		"processes": processes,
		"count":     len(processes),
	})
}

// parsePSAuxOutput parses `ps aux` output into a structured process list.
// Expected columns: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND
func parsePSAuxOutput(output string, filter string) []map[string]any {
	processes := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	lowerFilter := strings.ToLower(filter)

	// Skip header line.
	if scanner.Scan() {
		_ = scanner.Text()
	}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		name := strings.Join(fields[10:], " ")
		if filter != "" && !strings.Contains(strings.ToLower(name), lowerFilter) {
			continue
		}

		pid, _ := strconv.Atoi(fields[1])
		cpuPercent, _ := strconv.ParseFloat(fields[2], 64)
		rssKB, _ := strconv.ParseFloat(fields[5], 64)
		memoryMB := rssKB / 1024.0
		status := fields[7]

		processes = append(processes, map[string]any{
			"pid":         pid,
			"name":        name,
			"cpu_percent": cpuPercent,
			"memory_mb":   memoryMB,
			"status":      status,
		})
	}
	return processes
}

// parseTasklistOutput parses `tasklist /fo csv /nh` output into a structured process list.
// Expected CSV columns: "Image Name","PID","Session Name","Session#","Mem Usage"
func parseTasklistOutput(output string, filter string) []map[string]any {
	processes := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	lowerFilter := strings.ToLower(filter)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse CSV fields by splitting on "," and trimming quotes.
		fields := strings.Split(line, "\",\"")
		if len(fields) < 5 {
			continue
		}

		name := strings.Trim(fields[0], "\"")
		if filter != "" && !strings.Contains(strings.ToLower(name), lowerFilter) {
			continue
		}

		pidStr := strings.Trim(fields[1], "\"")
		pid, _ := strconv.Atoi(pidStr)

		memStr := strings.Trim(fields[4], "\"")
		memStr = strings.ReplaceAll(memStr, ",", "")
		memStr = strings.ReplaceAll(memStr, " K", "")
		memStr = strings.TrimSpace(memStr)
		memKB, _ := strconv.ParseFloat(memStr, 64)
		memoryMB := memKB / 1024.0

		processes = append(processes, map[string]any{
			"pid":         pid,
			"name":        name,
			"cpu_percent": 0.0,
			"memory_mb":   memoryMB,
			"status":      "running",
		})
	}
	return processes
}

// ---------------------------------------------------------------------------
// Handlers — env info tool
// ---------------------------------------------------------------------------

func handleNodesEnvInfo(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	homeDir, _ := os.UserHomeDir()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = os.Getenv("COMSPEC") // Windows fallback
	}
	term := os.Getenv("TERM")
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}

	pathDirs := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	pathDirsCount := len(pathDirs)
	if len(pathDirs) == 1 && pathDirs[0] == "" {
		pathDirsCount = 0
	}

	tempDir := os.TempDir()
	workingDir, _ := os.Getwd()

	return b.jsonResult(call, map[string]any{
		"user":            username,
		"home_dir":        homeDir,
		"shell":           shell,
		"term":            term,
		"lang":            lang,
		"path_dirs_count": pathDirsCount,
		"temp_dir":        tempDir,
		"working_dir":     workingDir,
	})
}

// ---------------------------------------------------------------------------
// Handlers — screen recording tool
// ---------------------------------------------------------------------------

func handleNodesScreenRecord(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output_path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: %w", err)
	}

	durationSec, err := intFrom(call.Input["duration_sec"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: %w", err)
	}
	if durationSec <= 0 {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: duration_sec must be positive")
	}
	if durationSec > nodesScreenRecordMaxDuration {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: duration_sec exceeds maximum of %d", nodesScreenRecordMaxDuration)
	}

	audio, _ := boolFrom(call.Input["audio"])

	fps, _ := intFrom(call.Input["fps"], nodesScreenRecordDefaultFPS)
	if fps <= 0 || fps > nodesScreenRecordMaxFPS {
		fps = nodesScreenRecordDefaultFPS
	}
	resolution := stringFromDefault(call.Input["resolution"], nodesScreenRecordDefaultResolution)
	display := stringFromDefault(call.Input["display"], "")
	quality := stringFromDefault(call.Input["quality"], "medium")

	// Map quality to ffmpeg preset/CRF.
	var ffmpegPreset, ffmpegCRF string
	switch quality {
	case "low":
		ffmpegPreset = "ultrafast"
		ffmpegCRF = "28"
	case "high":
		ffmpegPreset = "slow"
		ffmpegCRF = "18"
	default: // medium
		quality = "medium"
		ffmpegPreset = "fast"
		ffmpegCRF = "23"
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: %w", err)
	}

	recordTimeout := time.Duration(durationSec)*time.Second + nodesScreenRecordTimeoutBuffer
	execCtx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	var cmd *exec.Cmd
	platform := runtime.GOOS

	switch platform {
	case "darwin":
		args := []string{"-v", "-V", strconv.Itoa(durationSec)}
		if audio {
			args = append([]string{"-k"}, args...)
		}
		if display != "" {
			args = append(args, "-D", display)
		}
		args = append(args, absPath)
		cmd = exec.CommandContext(execCtx, "screencapture", args...)

	case "linux":
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: ffmpeg not found (install ffmpeg)")
		}
		displayID := display
		if displayID == "" {
			displayID = ":0.0"
		}
		args := []string{
			"-f", "x11grab",
			"-video_size", resolution,
			"-framerate", strconv.Itoa(fps),
			"-i", displayID,
			"-t", strconv.Itoa(durationSec),
			"-preset", ffmpegPreset,
			"-crf", ffmpegCRF,
			"-y",
		}
		if audio {
			args = append(args, "-f", "pulse", "-i", "default")
		}
		args = append(args, absPath)
		cmd = exec.CommandContext(execCtx, "ffmpeg", args...)

	case "windows":
		if _, lookErr := exec.LookPath("ffmpeg"); lookErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: ffmpeg not found (install ffmpeg)")
		}
		args := []string{
			"-f", "gdigrab",
			"-framerate", strconv.Itoa(fps),
			"-i", "desktop",
			"-t", strconv.Itoa(durationSec),
			"-preset", ffmpegPreset,
			"-crf", ffmpegCRF,
			"-y",
		}
		if audio {
			args = append(args, "-f", "dshow", "-i", "audio=virtual-audio-capturer")
		}
		args = append(args, absPath)
		cmd = exec.CommandContext(execCtx, "ffmpeg", args...)

	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: unsupported platform %q", platform)
	}

	if err := cmd.Run(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.screen_record: %w", err)
	}

	var sizeBytes int64
	if info, statErr := os.Stat(absPath); statErr == nil {
		sizeBytes = info.Size()
	}

	return b.jsonResult(call, map[string]any{
		"ok":           true,
		"path":         absPath,
		"size_bytes":   sizeBytes,
		"duration_sec": durationSec,
		"fps":          fps,
		"quality":      quality,
	})
}

// ---------------------------------------------------------------------------
// Handlers — battery status tool
// ---------------------------------------------------------------------------

func handleNodesBatteryStatus(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, nodesExecTimeout)
	defer cancel()

	platform := runtime.GOOS

	switch platform {
	case "darwin":
		return nodesBatteryDarwin(execCtx, b, call)
	case "linux":
		return nodesBatteryLinux(b, call)
	case "windows":
		return nodesBatteryWindows(execCtx, b, call)
	default:
		return contextengine.ToolResult{}, fmt.Errorf("nodes.battery_status: unsupported platform %q", platform)
	}
}

// nodesBatteryDarwin parses `pmset -g batt` output on macOS.
func nodesBatteryDarwin(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "pmset", "-g", "batt")
	out, err := cmd.Output()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.battery_status: %w", err)
	}

	output := string(out)
	percent := -1
	charging := false
	timeRemainingMin := -1
	powerSource := "unknown"

	// First line typically contains power source info.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "ac power") {
			powerSource = "ac"
		} else if strings.Contains(lower, "battery power") {
			powerSource = "battery"
		}

		// Parse percentage: look for "XX%"
		if idx := strings.Index(line, "%"); idx > 0 {
			// Walk back to find the start of the number.
			start := idx - 1
			for start >= 0 && line[start] >= '0' && line[start] <= '9' {
				start--
			}
			start++
			if start < idx {
				pct, parseErr := strconv.Atoi(line[start:idx])
				if parseErr == nil {
					percent = pct
				}
			}
		}

		if strings.Contains(lower, "charging") && !strings.Contains(lower, "discharging") &&
			!strings.Contains(lower, "not charging") {
			charging = true
		}

		// Parse time remaining: "X:XX remaining"
		if strings.Contains(lower, "remaining") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if strings.Contains(f, ":") && i+1 < len(fields) && strings.Contains(fields[i+1], "remaining") {
					parts := strings.SplitN(f, ":", 2)
					if len(parts) == 2 {
						hours, hErr := strconv.Atoi(parts[0])
						mins, mErr := strconv.Atoi(parts[1])
						if hErr == nil && mErr == nil {
							timeRemainingMin = hours*60 + mins
						}
					}
				}
			}
		}
	}

	return b.jsonResult(call, map[string]any{
		"percent":            percent,
		"charging":           charging,
		"time_remaining_min": timeRemainingMin,
		"power_source":       powerSource,
	})
}

// nodesBatteryLinux reads battery info from /sys/class/power_supply/.
func nodesBatteryLinux(b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	// Try common battery paths.
	batteryPaths := []string{
		"/sys/class/power_supply/BAT0",
		"/sys/class/power_supply/BAT1",
	}

	var batteryPath string
	for _, p := range batteryPaths {
		if _, err := os.Stat(p); err == nil {
			batteryPath = p
			break
		}
	}

	if batteryPath == "" {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.battery_status: no battery found")
	}

	percent := -1
	charging := false
	timeRemainingMin := -1
	powerSource := "battery"

	// Read capacity.
	if data, err := os.ReadFile(filepath.Join(batteryPath, "capacity")); err == nil {
		pct, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil {
			percent = pct
		}
	}

	// Read status.
	if data, err := os.ReadFile(filepath.Join(batteryPath, "status")); err == nil {
		status := strings.TrimSpace(strings.ToLower(string(data)))
		switch status {
		case "charging":
			charging = true
			powerSource = "ac"
		case "full":
			powerSource = "ac"
		case "discharging":
			powerSource = "battery"
		}
	}

	// Estimate time remaining from energy_now and power_now.
	energyNow := readSysfsInt(filepath.Join(batteryPath, "energy_now"))
	powerNow := readSysfsInt(filepath.Join(batteryPath, "power_now"))
	if energyNow > 0 && powerNow > 0 {
		hoursRemaining := float64(energyNow) / float64(powerNow)
		timeRemainingMin = int(hoursRemaining * 60)
	}

	return b.jsonResult(call, map[string]any{
		"percent":            percent,
		"charging":           charging,
		"time_remaining_min": timeRemainingMin,
		"power_source":       powerSource,
	})
}

// readSysfsInt reads an integer value from a sysfs file. Returns 0 on error.
func readSysfsInt(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// nodesBatteryWindows gets battery info via PowerShell WMI on Windows.
func nodesBatteryWindows(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	psScript := `$b = Get-WmiObject Win32_Battery; ` +
		`if ($b) { ` +
		`  $charging = $b.BatteryStatus -eq 2; ` +
		`  $remaining = $b.EstimatedRunTime; ` +
		`  Write-Output "$($b.EstimatedChargeRemaining),$charging,$remaining" ` +
		`} else { Write-Error 'no battery found' }`
	cmd := exec.CommandContext(ctx, "powershell", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("nodes.battery_status: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), ",", 3)
	percent := -1
	charging := false
	timeRemainingMin := -1
	powerSource := "unknown"

	if len(parts) >= 1 {
		pct, parseErr := strconv.Atoi(parts[0])
		if parseErr == nil {
			percent = pct
		}
	}
	if len(parts) >= 2 {
		charging = strings.EqualFold(parts[1], "true")
		if charging {
			powerSource = "ac"
		} else {
			powerSource = "battery"
		}
	}
	if len(parts) >= 3 {
		rem, parseErr := strconv.Atoi(parts[2])
		if parseErr == nil {
			timeRemainingMin = rem
		}
	}

	return b.jsonResult(call, map[string]any{
		"percent":            percent,
		"charging":           charging,
		"time_remaining_min": timeRemainingMin,
		"power_source":       powerSource,
	})
}
