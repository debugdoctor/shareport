package config

import "fmt"

type Config struct {
	DefaultPool string       `json:"default_pool"`
	Pools       []PoolConfig `json:"pools"`
}

type PoolConfig struct {
	Name   string   `json:"name"`
	Policy string   `json:"policy"`
	Links  []string `json:"links"`
}

// ErrNoPools signals the database has no pool configuration yet.
var ErrNoPools = fmt.Errorf("no pools configured")
