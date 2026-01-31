package runtime

import (
	"aimerick.com/shareport/components"
	"aimerick.com/shareport/config"
)

func PickPool(cfg config.Config, pools map[string]*components.Pool, name string) *components.Pool {
	if name == "" {
		name = cfg.DefaultPool
	}
	return pools[name]
}
