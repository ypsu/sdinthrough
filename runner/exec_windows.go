package runner

import (
	"os/exec"
	"strconv"
)

func executableSuffix() string { return ".exe" }

func setupCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		// /F = Force terminate, /T = Tree (terminates children too)
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
		return kill.Run()
	}
}
