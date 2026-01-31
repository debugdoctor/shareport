package runtime

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aimerick.com/shareport/config"
)

func proxyLogPath(dbPath string) string {
	baseDir := strings.TrimSpace(filepath.Dir(dbPath))
	if baseDir == "" || baseDir == "." {
		baseDir = ".shareport"
	}
	return filepath.Join(baseDir, "runtime.log")
}

// Backward compatibility alias
func xrayLogPath(dbPath string) string {
	baseDir := strings.TrimSpace(filepath.Dir(dbPath))
	if baseDir == "" || baseDir == "." {
		baseDir = ".shareport"
	}
	return proxyLogPath(dbPath)
}

func openProxyLogFile(dbPath string) (*os.File, error) {
	path := proxyLogPath(dbPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "\n--- shareport start proxy @ %s ---\n", time.Now().Format(time.RFC3339)); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func ResolveProxyBinary(xrayBin string) (string, error) {
	if xrayBin == "" {
		xrayBin = "xray"
	}
	if strings.ContainsRune(xrayBin, '/') || strings.ContainsRune(xrayBin, '\\') {
		return xrayBin, nil
	}
	return exec.LookPath(xrayBin)
}

func EnsureProxyInstalled(xrayBin string) (string, error) {
	if path, err := ResolveProxyBinary(xrayBin); err == nil {
		return path, nil
	}
	cmd := exec.Command("bash", "-c", "curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh | bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return exec.LookPath("xray")
}

// Backward compatibility aliases
func ResolveXrayBinary(xrayBin string) (string, error) {
	return ResolveProxyBinary(xrayBin)
}

func EnsureXrayInstalled(xrayBin string) (string, error) {
	return EnsureProxyInstalled(xrayBin)
}

func StartXray(dbPath, xrayPath, configPath string) error {
	return StartProxy(dbPath, xrayPath, configPath)
}

func StopXray(dbPath string) error {
	return StopProxy(dbPath)
}

func IsXrayRunning(dbPath string) bool {
	return IsProxyRunning(dbPath)
}

func SaveXrayPID(dbPath string, pid int) error {
	return SaveProxyPID(dbPath, pid)
}

func ReadXrayPID(dbPath string) (int, error) {
	return ReadProxyPID(dbPath)
}

func StartProxy(dbPath, proxyPath, configPath string) error {
	// Be explicit: in modern Xray, `run` is the default command, but flags are
	// tied to the run subcommand.
	cmd := exec.Command(proxyPath, "run", "-c", configPath)
	logFile, err := openProxyLogFile(dbPath)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = SaveProxyPID(dbPath, cmd.Process.Pid)
	}
	return nil
}

func SaveProxyPID(dbPath string, pid int) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return config.SetRuntimeState(db, "proxy_pid", strconv.Itoa(pid))
}

func ReadProxyPID(dbPath string) (int, error) {
	// Prefer DB state; fall back to legacy pidfile and migrate.
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	if v, ok, err := config.GetRuntimeState(db, "proxy_pid"); err != nil {
		return 0, err
	} else if ok {
		pid, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			_ = config.DeleteRuntimeState(db, "proxy_pid")
			return 0, err
		}
		return pid, nil
	}

	// Try legacy xray.pid for backward compatibility
	legacyPath := filepath.Join(".shareport", "xray.pid")
	data, err := os.ReadFile(legacyPath)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil {
			_ = config.SetRuntimeState(db, "proxy_pid", strconv.Itoa(pid))
			_ = os.Remove(legacyPath)
			return pid, nil
		}
	}
	return 0, nil
}

func StopProxy(dbPath string) error {
	pid, err := ReadProxyPID(dbPath)
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	db, err := config.OpenDB(dbPath)
	if err == nil {
		_ = config.DeleteRuntimeState(db, "proxy_pid")
		_ = db.Close()
	}
	_ = os.Remove(filepath.Join(".shareport", "xray.pid"))
	return nil
}

func IsProxyRunning(dbPath string) bool {
	pid, err := ReadProxyPID(dbPath)
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if proc.Signal(syscall.Signal(0)) == nil {
		return true
	}
	// If the PID is stale, clear it so next run doesn't "remember" a dead process.
	db, err := config.OpenDB(dbPath)
	if err == nil {
		_ = config.DeleteRuntimeState(db, "proxy_pid")
		_ = db.Close()
	}
	return false
}

// IsProxyListening checks whether the inbound port in the given Xray config is
// currently accepting TCP connections on localhost. This is a best-effort
// fallback for cases where the proxy was started outside shareport (so we don't
// have a tracked PID in the DB).
func IsProxyListening(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	inbounds, ok := root["inbounds"].([]any)
	if !ok || len(inbounds) == 0 {
		return false
	}

	listen := ""
	port := 0
	for _, it := range inbounds {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		proto, _ := m["protocol"].(string)
		switch strings.ToLower(strings.TrimSpace(proto)) {
		case "vless", "trojan":
		default:
			continue
		}
		if s, _ := m["listen"].(string); strings.TrimSpace(s) != "" {
			listen = strings.TrimSpace(s)
		}
		switch v := m["port"].(type) {
		case float64:
			port = int(v)
		case int:
			port = v
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				port = n
			}
		}
		break
	}
	if port <= 0 || port > 65535 {
		return false
	}

	dial := func(addr string) bool {
		conn, err := net.DialTimeout("tcp", addr, 450*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}

	// Prefer localhost since 0.0.0.0 also accepts it, and it works even when the
	// inbound listens on a specific public interface.
	if dial(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) {
		return true
	}
	if strings.TrimSpace(listen) != "" && listen != "0.0.0.0" && listen != "::" {
		if dial(net.JoinHostPort(listen, strconv.Itoa(port))) {
			return true
		}
	}
	return false
}

// ResolveRunningProxyBinary attempts to determine the actual proxy binary path
// for the currently running proxy process started by shareport.
func ResolveRunningProxyBinary(dbPath string) (string, error) {
	pid, err := ReadProxyPID(dbPath)
	if err != nil || pid <= 0 {
		if err == nil {
			err = fmt.Errorf("proxy not running")
		}
		return "", err
	}

	if runtime.GOOS == "linux" {
		if p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil && strings.TrimSpace(p) != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("cannot resolve running proxy binary path")
}
