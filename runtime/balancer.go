package runtime

import (
	"os"
	"os/exec"
	"time"
)

const (
	XrayAPIAddr        = "127.0.0.1:10085"
	XrayBalancerTag    = "balancer-0"
	DefaultOutboundTag = "node-1"
)

func runXrayBalancerOverride(xrayPath, apiAddr, balancerTag, outboundTag string) error {
	cmd := exec.Command(xrayPath, "api", "bo", "--server", apiAddr, "-b", balancerTag, outboundTag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func SetXrayBalancerOverride(xrayPath, apiAddr, balancerTag, outboundTag string) error {
	return runXrayBalancerOverride(xrayPath, apiAddr, balancerTag, outboundTag)
}

func SetXrayBalancerOverrideWithRetry(xrayPath, apiAddr, balancerTag, outboundTag string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := runXrayBalancerOverride(xrayPath, apiAddr, balancerTag, outboundTag); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	return lastErr
}

func EnsureDefaultOutbound(xrayPath string) error {
	return SetXrayBalancerOverrideWithRetry(xrayPath, XrayAPIAddr, XrayBalancerTag, DefaultOutboundTag, 3*time.Second)
}
