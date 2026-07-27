//go:build !windows

package proc

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// findClaude shells out to ps, which is present on both macOS and Linux and
// avoids a /proc parser that would only work on one of them.
func findClaude() ([]Process, error) {
	cmd := exec.Command("ps", "-A", "-o", "pid=,comm=")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list processes: %w: %s", err, strings.TrimSpace(errb.String()))
	}

	var res []Process
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sp := strings.Fields(line)
		if len(sp) < 2 {
			continue
		}
		pid, err := strconv.Atoi(sp[0])
		if err != nil {
			continue
		}
		// comm can be a full path depending on platform and invocation.
		name := filepath.Base(strings.Join(sp[1:], " "))
		if isClaude(name) {
			res = append(res, Process{PID: pid, Name: name})
		}
	}
	return res, nil
}
