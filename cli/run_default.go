//go:build !linux && !darwin

package cli

import (
	"os"
	"syscall"
)

func setSysProcAttr(attr *syscall.SysProcAttr) {}

func killProcess(process *os.Process, _ os.Signal) {
	process.Kill()
}
