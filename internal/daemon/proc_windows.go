//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow        = 0x08000000
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	belowNormalPriority   = 0x00004000
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow | detachedProcess | createNewProcessGroup | belowNormalPriority,
	}
}

func lowerPriority() {}
