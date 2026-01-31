package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aimerick.com/shareport/components"
	"aimerick.com/shareport/config"
)

const (
	rtBalancerDaemonPIDKey     = "balancer_daemon_pid"
	rtBalancerDaemonRunningKey = "balancer_daemon_running"
)

func IsBalancerDaemonRunning(dbPath string) bool {
	pid, err := readRuntimePID(dbPath, rtBalancerDaemonPIDKey)
	if err != nil || pid <= 0 {
		return false
	}
	if isZombiePID(pid) {
		_ = clearBalancerDaemonState(dbPath)
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = clearBalancerDaemonState(dbPath)
		return false
	}
	if proc.Signal(syscall.Signal(0)) == nil {
		return true
	}
	_ = clearBalancerDaemonState(dbPath)
	return false
}

func StartBalancerDaemon(dbPath, xrayBin string) (int, error) {
	if IsBalancerDaemonRunning(dbPath) {
		pid, _ := readRuntimePID(dbPath, rtBalancerDaemonPIDKey)
		return pid, nil
	}

	self, err := os.Executable()
	if err != nil {
		return 0, err
	}

	absDBPath := dbPath
	if strings.TrimSpace(absDBPath) != "" && !filepath.IsAbs(absDBPath) {
		if p, err := filepath.Abs(absDBPath); err == nil {
			absDBPath = p
		}
	}

	// Spawn a detached helper process that keeps switching in the background.
	cmd := exec.Command(self,
		"--balancer-daemon",
		"--db", absDBPath,
		"--xray-bin", xrayBin,
	)
	cmd.Stdin = mustOpenDevNull()
	cmd.Stdout = mustOpenBalancerDaemonLog(absDBPath)
	cmd.Stderr = cmd.Stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go reapBalancerDaemon(cmd, absDBPath)

	// Record child PID immediately; the child will also refresh this on startup.
	if err := writeRuntimePID(dbPath, rtBalancerDaemonPIDKey, cmd.Process.Pid); err != nil {
		// Best-effort cleanup.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return 0, err
	}
	_ = setRuntimeFlag(dbPath, rtBalancerDaemonRunningKey, true)
	return cmd.Process.Pid, nil
}

func StopBalancerDaemon(dbPath string) bool {
	pid, err := readRuntimePID(dbPath, rtBalancerDaemonPIDKey)
	if err != nil || pid <= 0 {
		_ = clearBalancerDaemonState(dbPath)
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = clearBalancerDaemonState(dbPath)
		return false
	}
	_ = proc.Signal(syscall.SIGTERM)
	_ = clearBalancerDaemonState(dbPath)
	return true
}

func RunBalancerDaemon(dbPath, xrayBin string) error {
	log.SetOutput(mustOpenBalancerDaemonLog(dbPath))
	log.SetPrefix("balancer-daemon ")

	// Mark ourselves running as early as possible.
	_ = writeRuntimePID(dbPath, rtBalancerDaemonPIDKey, os.Getpid())
	_ = setRuntimeFlag(dbPath, rtBalancerDaemonRunningKey, true)

	term := make(chan os.Signal, 2)
	reload := make(chan os.Signal, 2)
	signal.Notify(term, syscall.SIGTERM, syscall.SIGINT)
	signal.Notify(reload, syscall.SIGHUP)
	defer func() {
		signal.Stop(term)
		signal.Stop(reload)
	}()

	xrayPath, err := EnsureXrayInstalled(xrayBin)
	if err != nil {
		_ = clearBalancerDaemonState(dbPath)
		return err
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	eventCh := make(chan struct{}, 128)

	var pool *components.Pool
	var switchCfg BalancerSwitchConfig

	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	resetTimer := func(d time.Duration) {
		stopTimer()
		timer = time.NewTimer(d)
		timerC = timer.C
	}

	var stopTail chan struct{}
	var tailDone chan struct{}
	stopTailer := func() {
		if stopTail == nil {
			return
		}
		close(stopTail)
		<-tailDone
		stopTail = nil
		tailDone = nil
	}

	isBusinessConnLine := func(line string) bool {
		if !strings.Contains(line, " accepted ") {
			return false
		}
		if strings.Contains(line, "[api ->") {
			return false
		}
		return strings.Contains(line, "[inbound-") && strings.Contains(line, "-> node-")
	}

	startTailer := func() {
		if stopTail != nil {
			return
		}
		stopTail = make(chan struct{})
		tailDone = make(chan struct{})
		go func() {
			defer close(tailDone)
			path := xrayLogPath(dbPath)
			f, err := os.Open(path)
			if err != nil {
				log.Printf("tail xray log failed: %v", err)
				return
			}
			defer f.Close()

			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				log.Printf("seek xray log failed: %v", err)
				return
			}
			r := bufio.NewReader(f)
			for {
				select {
				case <-stopTail:
					return
				default:
				}

				line, err := r.ReadString('\n')
				if err != nil {
					if errors.Is(err, io.EOF) {
						time.Sleep(120 * time.Millisecond)
						continue
					}
					log.Printf("read xray log failed: %v", err)
					return
				}
				if !isBusinessConnLine(line) {
					continue
				}
				select {
				case eventCh <- struct{}{}:
				default:
				}
			}
		}()
	}

	switchOnce := func() {
		if pool == nil {
			return
		}
		_, idx := pool.Next()
		if idx < 0 {
			return
		}
		tag := "node-" + strconv.Itoa(idx+1)
		if err := SetXrayBalancerOverrideWithRetry(xrayPath, XrayAPIAddr, XrayBalancerTag, tag, 3*time.Second); err != nil {
			log.Printf("balancer override failed: %v", err)
			return
		}
		log.Printf("switched outbound to %s", tag)
	}

	ensureDefault := func() {
		if err := SetXrayBalancerOverrideWithRetry(xrayPath, XrayAPIAddr, XrayBalancerTag, DefaultOutboundTag, 3*time.Second); err != nil {
			log.Printf("set default outbound failed: %v", err)
			return
		}
		log.Printf("set default outbound to %s", DefaultOutboundTag)
	}

	reloadAll := func(reason string) error {
		nextCfg, err := loadConfigWithRetry(dbPath, 5*time.Second)
		if err != nil {
			return err
		}
		nextPools, err := components.BuildPools(nextCfg)
		if err != nil {
			return err
		}
		poolName := nextCfg.DefaultPool
		if poolName == "" && len(nextCfg.Pools) > 0 {
			poolName = nextCfg.Pools[0].Name
		}
		nextPool := PickPool(nextCfg, nextPools, poolName)
		if nextPool == nil {
			return errors.New("pool not found")
		}
		nextSwitchCfg, err := LoadBalancerSwitchConfigWithRetry(dbPath, 5*time.Second)
		if err != nil {
			return err
		}
		pool = nextPool
		switchCfg = nextSwitchCfg.Normalize()
		log.Printf(
			"reloaded (%s): pool=%s policy=%s size=%d mode=%s interval=%s interval_mode=%s min=%s max=%s",
			reason,
			pool.Name,
			pool.Policy,
			pool.Size(),
			switchCfg.Mode,
			switchCfg.Interval,
			switchCfg.IntervalMode,
			switchCfg.MinInterval,
			switchCfg.MaxInterval,
		)
		return nil
	}

	applyMode := func(reason string) {
		stopTimer()
		stopTailer()

		switch switchCfg.Mode {
		case BalancerSwitchOff:
			ensureDefault()
			return
		case BalancerSwitchPerConnection:
			startTailer()
			switchOnce()
			log.Printf("mode=%s (%s)", switchCfg.Mode, reason)
			return
		default:
			// interval
			switchOnce()
			d := switchCfg.NextInterval(rng)
			resetTimer(d)
			log.Printf("mode=%s next=%s (%s)", switchCfg.Mode, d, reason)
			return
		}
	}

	if err := reloadAll("startup"); err != nil {
		_ = clearBalancerDaemonState(dbPath)
		return err
	}
	applyMode("startup")

	for {
		select {
		case <-timerC:
			if switchCfg.Mode == BalancerSwitchInterval {
				switchOnce()
				d := switchCfg.NextInterval(rng)
				resetTimer(d)
				log.Printf("mode=%s next=%s (timer)", switchCfg.Mode, d)
			}
		case <-eventCh:
			if switchCfg.Mode == BalancerSwitchPerConnection {
				switchOnce()
			}
		case <-reload:
			if err := reloadAll("signal"); err != nil {
				log.Printf("reload failed: %v", err)
				continue
			}
			applyMode("signal")
		case <-term:
			stopTimer()
			stopTailer()
			_ = clearBalancerDaemonState(dbPath)
			return nil
		}
	}
}

func clearBalancerDaemonState(dbPath string) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_ = config.DeleteRuntimeState(db, rtBalancerDaemonPIDKey)
	_ = config.DeleteRuntimeState(db, rtBalancerDaemonRunningKey)
	return nil
}

func readRuntimePID(dbPath, key string) (int, error) {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	v, ok, err := config.GetRuntimeState(db, key)
	if err != nil || !ok {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		_ = config.DeleteRuntimeState(db, key)
		return 0, err
	}
	return pid, nil
}

func writeRuntimePID(dbPath, key string, pid int) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return config.SetRuntimeState(db, key, strconv.Itoa(pid))
}

func setRuntimeFlag(dbPath, key string, on bool) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if !on {
		return config.DeleteRuntimeState(db, key)
	}
	return config.SetRuntimeState(db, key, "1")
}

func mustOpenDevNull() *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return os.Stdin
	}
	return f
}

func balancerDaemonLogPath(dbPath string) string {
	baseDir := strings.TrimSpace(filepath.Dir(dbPath))
	if baseDir == "" || baseDir == "." {
		baseDir = ".shareport"
	}
	return filepath.Join(baseDir, "balancer-daemon.log")
}

func mustOpenBalancerDaemonLog(dbPath string) *os.File {
	path := balancerDaemonLogPath(dbPath)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return os.Stdout
	}
	_, _ = fmt.Fprintf(f, "\n--- balancer daemon @ %s ---\n", time.Now().Format(time.RFC3339))
	return f
}

func reapBalancerDaemon(cmd *exec.Cmd, dbPath string) {
	err := cmd.Wait()
	if cmd.Stdout != nil {
		if f, ok := cmd.Stdout.(*os.File); ok && f != os.Stdout && f != os.Stderr {
			_ = f.Close()
		}
	}
	if cmd.Stdin != nil {
		if f, ok := cmd.Stdin.(*os.File); ok && f != os.Stdin {
			_ = f.Close()
		}
	}
	if err != nil {
		log.Printf("balancer daemon exited: %v", err)
	}
	_ = clearBalancerDaemonState(dbPath)
}

func loadConfigWithRetry(dbPath string, timeout time.Duration) (config.Config, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := config.OpenDB(dbPath)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		cfg, err := config.LoadDB(db)
		_ = db.Close()
		if err == nil {
			return cfg, nil
		}
		lastErr = err
		if !isSQLiteBusy(err) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return config.Config{}, lastErr
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "database is locked") || strings.Contains(s, "database is busy")
}

func isZombiePID(pid int) bool {
	if pid <= 0 || runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// /proc/[pid]/stat: pid (comm) state ...
	// state is the first token after the last ')'.
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return false
	}
	state := s[i+2]
	return state == 'Z'
}
