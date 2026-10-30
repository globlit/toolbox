//go:build !linux && !darwin

package runner

import "os/exec"

// Windows uses CommandContext's process termination; credential helpers are prohibited.
func configureProcess(cmd *exec.Cmd) {}
