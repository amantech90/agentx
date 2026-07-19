//go:build windows

package childproc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Configure prevents console-based provider wrappers from opening a visible
// command window when Agent X is built as a Windows GUI application.
func Configure(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
