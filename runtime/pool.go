package runtime

import (
	"aimerick.com/shareport/config"
	"aimerick.com/shareport/lb"
)

func PickPool(cfg config.Config, pools map[string]*lb.Pool, name string) *lb.Pool {
	if name == "" {
		name = cfg.DefaultPool
	}
	return pools[name]
}
