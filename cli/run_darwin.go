package cli

import "syscall"

func setSysProcAttr(attr *syscall.SysProcAttr) {
	attr.Setpgid = true
}