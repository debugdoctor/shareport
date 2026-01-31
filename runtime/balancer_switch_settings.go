package runtime

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"aimerick.com/shareport/config"
)

type BalancerSwitchMode string

const (
	BalancerSwitchOff           BalancerSwitchMode = "off"
	BalancerSwitchInterval      BalancerSwitchMode = "interval"
	BalancerSwitchPerConnection BalancerSwitchMode = "per_connection"
)

type BalancerIntervalMode string

const (
	BalancerIntervalFixed  BalancerIntervalMode = "fixed"
	BalancerIntervalRandom BalancerIntervalMode = "random"
)

const (
	settingBalancerSwitchMode         = "balancer_switch_mode"
	settingBalancerIntervalMode       = "balancer_interval_mode"
	settingBalancerIntervalSeconds    = "balancer_interval_seconds"
	settingBalancerIntervalMinSeconds = "balancer_interval_min_seconds"
	settingBalancerIntervalMaxSeconds = "balancer_interval_max_seconds"
	minBalancerIntervalSeconds        = 10
	maxBalancerIntervalSeconds        = 30 * 60
	defaultBalancerIntervalSeconds    = 60
	defaultBalancerRandomMinSeconds   = 10
	defaultBalancerRandomMaxSeconds   = 60
)

type BalancerSwitchConfig struct {
	Mode         BalancerSwitchMode
	IntervalMode BalancerIntervalMode
	Interval     time.Duration
	MinInterval  time.Duration
	MaxInterval  time.Duration
}

func (c BalancerSwitchConfig) Normalize() BalancerSwitchConfig {
	out := c

	switch out.Mode {
	case BalancerSwitchOff, BalancerSwitchInterval, BalancerSwitchPerConnection:
	default:
		out.Mode = BalancerSwitchInterval
	}

	switch out.IntervalMode {
	case BalancerIntervalFixed, BalancerIntervalRandom:
	default:
		out.IntervalMode = BalancerIntervalFixed
	}

	clampSeconds := func(sec int) int {
		if sec < minBalancerIntervalSeconds {
			return minBalancerIntervalSeconds
		}
		if sec > maxBalancerIntervalSeconds {
			return maxBalancerIntervalSeconds
		}
		return sec
	}

	if out.Interval <= 0 {
		out.Interval = time.Duration(defaultBalancerIntervalSeconds) * time.Second
	}
	out.Interval = time.Duration(clampSeconds(int(out.Interval.Seconds()))) * time.Second

	if out.MinInterval <= 0 {
		out.MinInterval = time.Duration(defaultBalancerRandomMinSeconds) * time.Second
	}
	if out.MaxInterval <= 0 {
		out.MaxInterval = time.Duration(defaultBalancerRandomMaxSeconds) * time.Second
	}
	minSec := clampSeconds(int(out.MinInterval.Seconds()))
	maxSec := clampSeconds(int(out.MaxInterval.Seconds()))
	if minSec > maxSec {
		minSec, maxSec = maxSec, minSec
	}
	out.MinInterval = time.Duration(minSec) * time.Second
	out.MaxInterval = time.Duration(maxSec) * time.Second

	return out
}

func (c BalancerSwitchConfig) NextInterval(rng *rand.Rand) time.Duration {
	c = c.Normalize()
	if c.IntervalMode != BalancerIntervalRandom {
		return c.Interval
	}
	minSec := int(c.MinInterval.Seconds())
	maxSec := int(c.MaxInterval.Seconds())
	if minSec >= maxSec {
		return c.MinInterval
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return time.Duration(minSec+rng.Intn(maxSec-minSec+1)) * time.Second
}

func LoadBalancerSwitchConfigWithRetry(dbPath string, timeout time.Duration) (BalancerSwitchConfig, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := config.OpenDB(dbPath)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}

		modeStr, _, err1 := config.GetSetting(db, settingBalancerSwitchMode)
		intervalModeStr, _, err2 := config.GetSetting(db, settingBalancerIntervalMode)
		intervalSecStr, _, err3 := config.GetSetting(db, settingBalancerIntervalSeconds)
		minSecStr, _, err4 := config.GetSetting(db, settingBalancerIntervalMinSeconds)
		maxSecStr, _, err5 := config.GetSetting(db, settingBalancerIntervalMaxSeconds)
		_ = db.Close()

		if err := firstErr(err1, err2, err3, err4, err5); err != nil {
			lastErr = err
			if !isSQLiteBusy(err) {
				break
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}

		cfg := BalancerSwitchConfig{
			Mode:         BalancerSwitchMode(strings.TrimSpace(modeStr)),
			IntervalMode: BalancerIntervalMode(strings.TrimSpace(intervalModeStr)),
		}
		if sec, err := strconv.Atoi(strings.TrimSpace(intervalSecStr)); err == nil && sec > 0 {
			cfg.Interval = time.Duration(sec) * time.Second
		}
		if sec, err := strconv.Atoi(strings.TrimSpace(minSecStr)); err == nil && sec > 0 {
			cfg.MinInterval = time.Duration(sec) * time.Second
		}
		if sec, err := strconv.Atoi(strings.TrimSpace(maxSecStr)); err == nil && sec > 0 {
			cfg.MaxInterval = time.Duration(sec) * time.Second
		}
		return cfg.Normalize(), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("load balancer switch config timeout")
	}
	return BalancerSwitchConfig{}, lastErr
}

func SaveBalancerSwitchConfig(dbPath string, cfg BalancerSwitchConfig) error {
	cfg = cfg.Normalize()
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := config.SetSetting(db, settingBalancerSwitchMode, string(cfg.Mode)); err != nil {
		return err
	}
	if err := config.SetSetting(db, settingBalancerIntervalMode, string(cfg.IntervalMode)); err != nil {
		return err
	}
	if err := config.SetSetting(db, settingBalancerIntervalSeconds, strconv.Itoa(int(cfg.Interval.Seconds()))); err != nil {
		return err
	}
	if err := config.SetSetting(db, settingBalancerIntervalMinSeconds, strconv.Itoa(int(cfg.MinInterval.Seconds()))); err != nil {
		return err
	}
	return config.SetSetting(db, settingBalancerIntervalMaxSeconds, strconv.Itoa(int(cfg.MaxInterval.Seconds())))
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
