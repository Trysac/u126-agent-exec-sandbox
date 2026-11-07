//go:build !unix

package agentexecsandbox

import "os/exec"

func configureSysProc(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}
