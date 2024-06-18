package cli

import "os"

func killProcess(process *os.Process, _ os.Signal) {
	process.Kill()
}
