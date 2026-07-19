//go:build windows

package childproc

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigurePreventsConsoleWindows(t *testing.T) {
	t.Parallel()

	command := exec.Command("provider.exe", "--version")
	Configure(command)
	if command.SysProcAttr == nil {
		t.Fatal("Configure() did not set Windows process attributes")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("Configure() did not hide the child window")
	}
	if command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("Configure() did not prevent console allocation")
	}
}
