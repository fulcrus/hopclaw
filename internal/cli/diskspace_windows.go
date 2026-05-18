//go:build windows

package cli

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func getFreeDiskSpace(path string) (uint64, error) {
	// Use wmic to get free space on Windows.
	drive := strings.ToUpper(path[:1])
	cmd := exec.Command("wmic", "logicaldisk", "where", fmt.Sprintf("DeviceID='%s:'", drive), "get", "FreeSpace", "/value")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("wmic query failed: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FreeSpace=") {
			val := strings.TrimPrefix(line, "FreeSpace=")
			return strconv.ParseUint(strings.TrimSpace(val), 10, 64)
		}
	}
	return 0, fmt.Errorf("could not parse free space from wmic output")
}
