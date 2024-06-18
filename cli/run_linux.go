package cli

import (
	"os"
	"syscall"
)

func setSysProcAttr(attr *syscall.SysProcAttr) {
	attr.Setpgid = true
	attr.Pdeathsig = syscall.SIGTERM
}

func killProcess(process *os.Process, signal os.Signal) {
	// Sending the signal to -pid sends it to all processes
	// in the process group.
	_ = syscall.Kill(-process.Pid, signal.(syscall.Signal))
}
