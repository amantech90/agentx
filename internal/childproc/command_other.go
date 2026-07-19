//go:build !windows

package childproc

import "os/exec"

// Configure applies platform-specific child process settings.
func Configure(*exec.Cmd) {}
