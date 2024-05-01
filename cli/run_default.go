//go:build !linux

package cli

import "syscall"

func setSysProcAttr(attr *syscall.SysProcAttr) {}
