//go:build !linux && !darwin

package cli

import "syscall"

func setSysProcAttr(attr *syscall.SysProcAttr) {}
