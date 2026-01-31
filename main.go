package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"aimerick.com/shareport/cli"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/lb"
	"aimerick.com/shareport/runtime"
	"aimerick.com/shareport/ui"
)

func main() {
	var (
		printNext   bool
		generate    bool
		balancerD   bool
		dbPath      string
		initWizard  bool
		proxyConfig  string
		proxyBin     string
		lang        string
	)

	flag.BoolVar(&printNext, "print-next", false, "print next backend link and exit")
	flag.BoolVar(&generate, "generate", false, "interactive Xray config generator")
	flag.BoolVar(&balancerD, "balancer-daemon", false, "run balancer scheduler daemon (internal)")
	flag.StringVar(&dbPath, "db", ".shareport/shareport.db", "sqlite database path (optional)")
	flag.BoolVar(&initWizard, "init", false, "force interactive init wizard to recreate config")
	flag.StringVar(&proxyConfig, "xray-config", ".shareport/config.json", "proxy config output path")
	flag.StringVar(&proxyBin, "xray-bin", "xray", "proxy binary path or name")
	flag.StringVar(&lang, "lang", "", "language (zh or en)")
	flag.Parse()

	if !filepath.IsAbs(dbPath) {
		if p, err := filepath.Abs(dbPath); err == nil {
			dbPath = p
		}
	}

	if lang == "" {
		lang = strings.TrimSpace(os.Getenv("SHAREPORT_LANG"))
	}
	if lang == "" {
		lang = "zh"
	}
	msgs, err := i18n.LoadMessages(lang)
	if err != nil {
		log.Fatalf("load i18n failed: %v", err)
	}
	term := ui.NewTUI()

	if balancerD {
		// This is spawned by the UI in a detached process; it should not prompt.
		if err := runtime.RunBalancerDaemon(dbPath, proxyBin); err != nil {
			log.Fatalf("balancer daemon failed: %v", err)
		}
		return
	}

	if generate {
		cfg, _, err := cli.LoadConfigOrInit(term, msgs, dbPath, initWizard)
		if err != nil {
			log.Fatalf("%s: %v", msgs.Get("load_config_failed"), err)
		}
		if err := cli.RunXraySetup(term, msgs, cfg, dbPath, proxyConfig, proxyBin); err != nil {
			log.Fatalf("%s: %v", msgs.Get("generate_failed"), err)
		}
		return
	}

	cfg, didInit, err := cli.LoadConfigOrInit(term, msgs, dbPath, initWizard)
	if err != nil {
		log.Fatalf("%s: %v", msgs.Get("load_config_failed"), err)
	}

	if didInit {
		if err := cli.RunXraySetup(term, msgs, cfg, dbPath, proxyConfig, proxyBin); err != nil {
			log.Fatalf("%s: %v", msgs.Get("generate_failed"), err)
		}
	}

	pools, err := lb.BuildPools(cfg)
	if err != nil {
		log.Fatalf("%s: %v", msgs.Get("build_pools_failed"), err)
	}

	if printNext {
		pool := runtime.PickPool(cfg, pools, cfg.DefaultPool)
		if pool == nil {
			log.Fatalf("%s: %s", msgs.Get("pool_not_found"), cfg.DefaultPool)
		}
		link, idx := pool.Next()
		fmt.Println(link)
		_ = idx
		return
	}

	if err := cli.ManageConfig(term, msgs, cfg, dbPath, proxyConfig, proxyBin); err != nil {
		log.Fatalf("manage config failed: %v", err)
	}
}
