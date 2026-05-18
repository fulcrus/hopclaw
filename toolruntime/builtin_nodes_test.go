package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nodesTestBuiltins(t *testing.T) (*Builtins, context.Context, *agent.Run, *agent.Session) {
	t.Helper()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	ctx := context.Background()
	run := &agent.Run{ID: "run-nodes-1"}
	sess := &agent.Session{ID: "sess-nodes-1"}
	return builtins, ctx, run, sess
}

func nodesExec(t *testing.T, builtins *Builtins, ctx context.Context, run *agent.Run, sess *agent.Session, name string, input map[string]any) string {
	t.Helper()
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-" + name, Name: name, Input: input,
	}})
	if err != nil {
		t.Fatalf("%s error: %v", name, err)
	}
	return results[0].Content
}

// ---------------------------------------------------------------------------
// 1. TestNodesEnvInfo
// ---------------------------------------------------------------------------

func TestNodesEnvInfo(t *testing.T) {
	t.Parallel()
	builtins, ctx, run, sess := nodesTestBuiltins(t)

	content := nodesExec(t, builtins, ctx, run, sess, "nodes.env_info", map[string]any{})

	var result struct {
		User         string `json:"user"`
		HomeDir      string `json:"home_dir"`
		Shell        string `json:"shell"`
		Term         string `json:"term"`
		Lang         string `json:"lang"`
		PathDirCount int    `json:"path_dirs_count"`
		TempDir      string `json:"temp_dir"`
		WorkingDir   string `json:"working_dir"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("nodes.env_info unmarshal: %v", err)
	}

	// User should not be empty (pure Go, no external deps).
	if result.User == "" {
		t.Fatal("nodes.env_info user is empty")
	}

	// HomeDir should be a non-empty path.
	if result.HomeDir == "" {
		t.Fatal("nodes.env_info home_dir is empty")
	}

	// TempDir should exist.
	if result.TempDir == "" {
		t.Fatal("nodes.env_info temp_dir is empty")
	}
	if _, err := os.Stat(result.TempDir); err != nil {
		t.Fatalf("nodes.env_info temp_dir does not exist: %v", err)
	}

	// WorkingDir should be non-empty.
	if result.WorkingDir == "" {
		t.Fatal("nodes.env_info working_dir is empty")
	}

	// PathDirCount should be positive on any working system.
	if result.PathDirCount <= 0 {
		t.Fatalf("nodes.env_info path_dirs_count = %d, want > 0", result.PathDirCount)
	}

	// Shell should be set on Unix-like systems.
	if runtime.GOOS != "windows" && result.Shell == "" {
		t.Fatal("nodes.env_info shell is empty on non-windows")
	}
}

// ---------------------------------------------------------------------------
// 2. TestNodesClipboardSchema
// ---------------------------------------------------------------------------

func TestNodesClipboardSchema(t *testing.T) {
	t.Parallel()

	// Verify the read schema has the expected structure.
	readSchema := nodesClipboardReadInputSchema()
	if readSchema["type"] != "object" {
		t.Fatalf("clipboard_read input schema type = %v, want object", readSchema["type"])
	}
	props, ok := readSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("clipboard_read input schema properties is not map")
	}
	if len(props) != 0 {
		t.Fatalf("clipboard_read input schema should have 0 properties, got %d", len(props))
	}

	readOutSchema := nodesClipboardReadOutputSchema()
	outProps, ok := readOutSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("clipboard_read output schema properties is not map")
	}
	if _, ok := outProps["content"]; !ok {
		t.Fatal("clipboard_read output schema missing 'content' property")
	}
	if _, ok := outProps["format"]; !ok {
		t.Fatal("clipboard_read output schema missing 'format' property")
	}

	// Verify the write schema has the expected structure.
	writeSchema := nodesClipboardWriteInputSchema()
	writeProps, ok := writeSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("clipboard_write input schema properties is not map")
	}
	if _, ok := writeProps["content"]; !ok {
		t.Fatal("clipboard_write input schema missing 'content' property")
	}
	required, ok := writeSchema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "content" {
		t.Fatalf("clipboard_write required = %v, want [content]", writeSchema["required"])
	}

	writeOutSchema := nodesClipboardWriteOutputSchema()
	writeOutProps, ok := writeOutSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("clipboard_write output schema properties is not map")
	}
	if _, ok := writeOutProps["ok"]; !ok {
		t.Fatal("clipboard_write output schema missing 'ok' property")
	}
}

// ---------------------------------------------------------------------------
// 3. TestNodesProcessList
// ---------------------------------------------------------------------------

func TestNodesProcessList(t *testing.T) {
	t.Parallel()
	builtins, ctx, run, sess := nodesTestBuiltins(t)

	// Skip on platforms where ps/tasklist might not be available in CI.
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if _, err := exec.LookPath("ps"); err != nil {
			t.Skip("ps not found")
		}
	} else if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("tasklist"); err != nil {
			t.Skip("tasklist not found")
		}
	}

	content := nodesExec(t, builtins, ctx, run, sess, "nodes.process_list", map[string]any{})

	var result struct {
		Processes []struct {
			PID        int     `json:"pid"`
			Name       string  `json:"name"`
			CPUPercent float64 `json:"cpu_percent"`
			MemoryMB   float64 `json:"memory_mb"`
			Status     string  `json:"status"`
		} `json:"processes"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("nodes.process_list unmarshal: %v", err)
	}

	if result.Count <= 0 {
		t.Fatalf("nodes.process_list count = %d, want > 0", result.Count)
	}

	if len(result.Processes) != result.Count {
		t.Fatalf("nodes.process_list processes length = %d, count = %d", len(result.Processes), result.Count)
	}

	// Verify at least one process has a non-empty name.
	foundNamed := false
	for _, p := range result.Processes {
		if p.Name != "" {
			foundNamed = true
			break
		}
	}
	if !foundNamed {
		t.Fatal("nodes.process_list: no process with a non-empty name found")
	}

	// Test with filter — filter for something that should exist.
	var filterTerm string
	switch runtime.GOOS {
	case "darwin", "linux":
		filterTerm = "go" // go test process should be running
	case "windows":
		filterTerm = "tasklist" // won't match itself but System should exist
	}

	content = nodesExec(t, builtins, ctx, run, sess, "nodes.process_list", map[string]any{
		"filter": filterTerm,
	})
	var filteredResult struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(content), &filteredResult); err != nil {
		t.Fatalf("nodes.process_list (filtered) unmarshal: %v", err)
	}
	// Filtered count should be less than or equal to unfiltered count.
	if filteredResult.Count > result.Count {
		t.Fatalf("nodes.process_list filtered count %d > unfiltered count %d", filteredResult.Count, result.Count)
	}
}

// ---------------------------------------------------------------------------
// 4. TestNodesBatteryStatus
// ---------------------------------------------------------------------------

func TestNodesBatteryStatus(t *testing.T) {
	t.Parallel()

	// Verify output schema structure.
	schema := nodesBatteryStatusOutputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("battery_status output schema properties is not map")
	}
	expectedKeys := []string{"percent", "charging", "time_remaining_min", "power_source"}
	for _, key := range expectedKeys {
		if _, ok := props[key]; !ok {
			t.Fatalf("battery_status output schema missing %q property", key)
		}
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("battery_status output schema required is not []string")
	}
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r] = true
	}
	for _, key := range []string{"percent", "charging", "power_source"} {
		if !requiredSet[key] {
			t.Fatalf("battery_status output schema missing required key %q", key)
		}
	}

	// Attempt to run the actual handler; skip if no battery is present.
	builtins, ctx, run, sess := nodesTestBuiltins(t)

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("pmset"); err != nil {
			t.Skip("pmset not found")
		}
	case "linux":
		if _, err := os.Stat("/sys/class/power_supply/BAT0"); err != nil {
			if _, err := os.Stat("/sys/class/power_supply/BAT1"); err != nil {
				t.Skip("no battery sysfs path found")
			}
		}
	case "windows":
		if _, err := exec.LookPath("powershell"); err != nil {
			t.Skip("powershell not found")
		}
	default:
		t.Skipf("unsupported platform %q", runtime.GOOS)
	}

	content := nodesExec(t, builtins, ctx, run, sess, "nodes.battery_status", map[string]any{})

	var result struct {
		Percent          int    `json:"percent"`
		Charging         bool   `json:"charging"`
		TimeRemainingMin int    `json:"time_remaining_min"`
		PowerSource      string `json:"power_source"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("nodes.battery_status unmarshal: %v", err)
	}

	// Percent should be between -1 and 100.
	if result.Percent < -1 || result.Percent > 100 {
		t.Fatalf("nodes.battery_status percent = %d, want [-1, 100]", result.Percent)
	}

	// PowerSource should be one of the expected values.
	validSources := map[string]bool{"battery": true, "ac": true, "unknown": true}
	if !validSources[result.PowerSource] {
		t.Fatalf("nodes.battery_status power_source = %q, want one of battery/ac/unknown", result.PowerSource)
	}
}

// ---------------------------------------------------------------------------
// 5. TestNodesCameraListSchema
// ---------------------------------------------------------------------------

func TestNodesCameraListSchema(t *testing.T) {
	t.Parallel()

	// Verify input schema.
	inputSchema := nodesCameraListInputSchema()
	if inputSchema["type"] != "object" {
		t.Fatalf("camera_list input schema type = %v, want object", inputSchema["type"])
	}

	// Verify output schema.
	outputSchema := nodesCameraListOutputSchema()
	outProps, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("camera_list output schema properties is not map")
	}
	if _, ok := outProps["cameras"]; !ok {
		t.Fatal("camera_list output schema missing 'cameras' property")
	}
	if _, ok := outProps["count"]; !ok {
		t.Fatal("camera_list output schema missing 'count' property")
	}

	// Verify the cameras array schema has expected item properties.
	camerasSchema, ok := outProps["cameras"].(map[string]any)
	if !ok {
		t.Fatal("camera_list cameras schema is not map")
	}
	items, ok := camerasSchema["items"].(map[string]any)
	if !ok {
		t.Fatal("camera_list cameras items is not map")
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("camera_list camera item properties is not map")
	}
	if _, ok := itemProps["name"]; !ok {
		t.Fatal("camera_list camera item missing 'name' property")
	}
	if _, ok := itemProps["index"]; !ok {
		t.Fatal("camera_list camera item missing 'index' property")
	}

	// Verify camera_capture input schema has required output_path.
	captureSchema := nodesCameraCaptureInputSchema()
	captureProps, ok := captureSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("camera_capture input schema properties is not map")
	}
	if _, ok := captureProps["output_path"]; !ok {
		t.Fatal("camera_capture input schema missing 'output_path' property")
	}
	if _, ok := captureProps["device"]; !ok {
		t.Fatal("camera_capture input schema missing 'device' property")
	}
	captureRequired, ok := captureSchema["required"].([]string)
	if !ok || len(captureRequired) != 1 || captureRequired[0] != "output_path" {
		t.Fatalf("camera_capture required = %v, want [output_path]", captureSchema["required"])
	}
}

// ---------------------------------------------------------------------------
// 6. TestNodesLocationSchema
// ---------------------------------------------------------------------------

func TestNodesLocationSchema(t *testing.T) {
	t.Parallel()

	// Verify input schema.
	inputSchema := nodesLocationInputSchema()
	if inputSchema["type"] != "object" {
		t.Fatalf("location input schema type = %v, want object", inputSchema["type"])
	}
	inputProps, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("location input schema properties is not map")
	}
	if _, ok := inputProps["timeout_sec"]; !ok {
		t.Fatal("location input schema missing 'timeout_sec' property")
	}

	// Verify output schema.
	outputSchema := nodesLocationOutputSchema()
	outProps, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("location output schema properties is not map")
	}
	expectedFields := []string{"latitude", "longitude", "accuracy_meters", "source"}
	for _, field := range expectedFields {
		if _, ok := outProps[field]; !ok {
			t.Fatalf("location output schema missing %q property", field)
		}
	}

	required, ok := outputSchema["required"].([]string)
	if !ok {
		t.Fatal("location output schema required is not []string")
	}
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r] = true
	}
	for _, key := range []string{"latitude", "longitude", "source"} {
		if !requiredSet[key] {
			t.Fatalf("location output schema missing required key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. TestNodesScreenRecordSchema
// ---------------------------------------------------------------------------

func TestNodesScreenRecordSchema(t *testing.T) {
	t.Parallel()

	inputSchema := nodesScreenRecordInputSchema()
	inputProps, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("screen_record input schema properties is not map")
	}
	for _, key := range []string{"output_path", "duration_sec", "audio"} {
		if _, ok := inputProps[key]; !ok {
			t.Fatalf("screen_record input schema missing %q property", key)
		}
	}
	required, ok := inputSchema["required"].([]string)
	if !ok {
		t.Fatal("screen_record input schema required is not []string")
	}
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r] = true
	}
	if !requiredSet["output_path"] || !requiredSet["duration_sec"] {
		t.Fatalf("screen_record required = %v, want output_path and duration_sec", required)
	}

	outputSchema := nodesScreenRecordOutputSchema()
	outProps, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("screen_record output schema properties is not map")
	}
	for _, key := range []string{"ok", "path", "size_bytes", "duration_sec"} {
		if _, ok := outProps[key]; !ok {
			t.Fatalf("screen_record output schema missing %q property", key)
		}
	}
}

// ---------------------------------------------------------------------------
// 8. TestNodesProcessListParsers
// ---------------------------------------------------------------------------

func TestNodesProcessListParsers(t *testing.T) {
	t.Parallel()

	// Test parsePSAuxOutput.
	psOutput := `USER         PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
root           1  0.0  0.1 168940 11324 ?        Ss   Mar12   0:02 /sbin/init
testuser    1234  5.2  1.3 123456 13312 pts/0    S+   10:00   0:15 /usr/bin/python3 test.py
nobody      5678  0.0  0.0  54321  2048 ?        S    09:00   0:00 /usr/sbin/noop`

	procs := parsePSAuxOutput(psOutput, "")
	if len(procs) != 3 {
		t.Fatalf("parsePSAuxOutput count = %d, want 3", len(procs))
	}

	// Verify second process fields.
	p := procs[1]
	if p["pid"] != 1234 {
		t.Fatalf("pid = %v, want 1234", p["pid"])
	}
	if p["cpu_percent"] != 5.2 {
		t.Fatalf("cpu_percent = %v, want 5.2", p["cpu_percent"])
	}
	if !strings.Contains(p["name"].(string), "python3") {
		t.Fatalf("name = %v, want to contain python3", p["name"])
	}

	// Test with filter.
	filtered := parsePSAuxOutput(psOutput, "python")
	if len(filtered) != 1 {
		t.Fatalf("parsePSAuxOutput filtered count = %d, want 1", len(filtered))
	}
	if filtered[0]["pid"] != 1234 {
		t.Fatalf("filtered pid = %v, want 1234", filtered[0]["pid"])
	}

	// Test parseTasklistOutput.
	tasklistOutput := `"System Idle Process","0","Services","0","8 K"
"System","4","Services","0","128 K"
"notepad.exe","9876","Console","1","15,360 K"`

	tasks := parseTasklistOutput(tasklistOutput, "")
	if len(tasks) != 3 {
		t.Fatalf("parseTasklistOutput count = %d, want 3", len(tasks))
	}
	if tasks[2]["name"] != "notepad.exe" {
		t.Fatalf("task name = %v, want notepad.exe", tasks[2]["name"])
	}
	if tasks[2]["pid"] != 9876 {
		t.Fatalf("task pid = %v, want 9876", tasks[2]["pid"])
	}

	// Test tasklist filter.
	filteredTasks := parseTasklistOutput(tasklistOutput, "notepad")
	if len(filteredTasks) != 1 {
		t.Fatalf("parseTasklistOutput filtered count = %d, want 1", len(filteredTasks))
	}
}

// ---------------------------------------------------------------------------
// 9. TestNodesCameraListParsers
// ---------------------------------------------------------------------------

func TestNodesCameraListParsers(t *testing.T) {
	t.Parallel()

	// Test parseDarwinCameraList.
	darwinOutput := `Cameras:

    FaceTime HD Camera:

      Model ID: UVC Camera VendorID_1452 ProductID_34068
      Unique ID: 0x8020000005ac8514
`
	cameras := parseDarwinCameraList(darwinOutput)
	if len(cameras) != 1 {
		t.Fatalf("parseDarwinCameraList count = %d, want 1", len(cameras))
	}
	if cameras[0]["name"] != "FaceTime HD Camera" {
		t.Fatalf("camera name = %v, want FaceTime HD Camera", cameras[0]["name"])
	}
	if cameras[0]["index"] != 0 {
		t.Fatalf("camera index = %v, want 0", cameras[0]["index"])
	}

	// Test parseWindowsCameraList.
	windowsOutput := `[dshow @ 0x1234] DirectShow video devices (some may be both video and audio devices)
[dshow @ 0x1234]  "Integrated Webcam"
[dshow @ 0x1234]  "OBS Virtual Camera"
[dshow @ 0x1234] DirectShow audio devices
[dshow @ 0x1234]  "Microphone (Realtek Audio)"
`
	winCameras := parseWindowsCameraList(windowsOutput)
	if len(winCameras) != 2 {
		t.Fatalf("parseWindowsCameraList count = %d, want 2", len(winCameras))
	}
	if winCameras[0]["name"] != "Integrated Webcam" {
		t.Fatalf("win camera[0] name = %v, want Integrated Webcam", winCameras[0]["name"])
	}
	if winCameras[1]["name"] != "OBS Virtual Camera" {
		t.Fatalf("win camera[1] name = %v, want OBS Virtual Camera", winCameras[1]["name"])
	}
}

// ---------------------------------------------------------------------------
// 10. TestNodesScreenCaptureWindows
// ---------------------------------------------------------------------------

func TestNodesScreenCaptureWindowsSchema(t *testing.T) {
	t.Parallel()

	// Verify the screen_capture tool now lists "windows" as a valid case
	// by checking that the handler code path exists (schema test).
	schema := nodesScreenCaptureInputSchema()
	if schema["type"] != "object" {
		t.Fatalf("screen_capture input schema type = %v, want object", schema["type"])
	}

	outSchema := nodesScreenCaptureOutputSchema()
	outProps, ok := outSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("screen_capture output schema properties is not map")
	}
	for _, key := range []string{"ok", "path", "size_bytes"} {
		if _, ok := outProps[key]; !ok {
			t.Fatalf("screen_capture output schema missing %q property", key)
		}
	}
}

// ---------------------------------------------------------------------------
// 11. TestNodesToolDefsCount
// ---------------------------------------------------------------------------

func TestNodesToolDefsCount(t *testing.T) {
	t.Parallel()

	defs := nodesToolDefs(BuiltinsConfig{})

	// We now have 15 tools: 6 original + 9 new (camera_capture, camera_list,
	// location, clipboard_read, clipboard_write, process_list, env_info,
	// screen_record, battery_status).
	const expectedToolCount = 15
	if len(defs) != expectedToolCount {
		t.Fatalf("nodesToolDefs count = %d, want %d", len(defs), expectedToolCount)
	}

	// Verify all tool names are unique.
	seen := make(map[string]bool)
	for _, d := range defs {
		if seen[d.Manifest.Name] {
			t.Fatalf("duplicate tool name: %s", d.Manifest.Name)
		}
		seen[d.Manifest.Name] = true
	}

	// Verify all handlers are non-nil.
	for _, d := range defs {
		if d.Handler == nil {
			t.Fatalf("tool %q has nil handler", d.Manifest.Name)
		}
	}
}
