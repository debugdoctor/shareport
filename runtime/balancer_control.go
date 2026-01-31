package runtime

import (
	"fmt"
	"os"
	"syscall"
)

func ReloadBalancerDaemon(dbPath string) error {
	if !IsBalancerDaemonRunning(dbPath) {
		return fmt.Errorf("balancer daemon not running")
	}
	pid, err := readRuntimePID(dbPath, rtBalancerDaemonPIDKey)
	if err != nil || pid <= 0 {
		return fmt.Errorf("balancer daemon not running")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGHUP)
}
