package components

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"aimerick.com/shareport/config"
)

type Pool struct {
	Name   string
	Policy string
	Links  []string

	mu     sync.Mutex
	idx    int
	counts []int
	rng    *rand.Rand
}

func BuildPools(cfg config.Config) (map[string]*Pool, error) {
	pools := make(map[string]*Pool)
	for _, p := range cfg.Pools {
		if p.Name == "" {
			return nil, fmt.Errorf("pool name is required")
		}
		if len(p.Links) == 0 {
			return nil, fmt.Errorf("pool %s has no links", p.Name)
		}
		if p.Policy == "" {
			p.Policy = "round_robin"
		}
		switch p.Policy {
		case "round_robin", "random", "least_conn":
		default:
			return nil, fmt.Errorf("pool %s has unsupported policy: %s", p.Name, p.Policy)
		}
		for _, link := range p.Links {
			if _, err := Parse(link); err != nil {
				return nil, fmt.Errorf("pool %s has invalid link: %v", p.Name, err)
			}
		}
		pools[p.Name] = &Pool{
			Name:   p.Name,
			Policy: p.Policy,
			Links:  p.Links,
			counts: make([]int, len(p.Links)),
			rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		}
	}
	return pools, nil
}

func (p *Pool) Next() (string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.Links) == 0 {
		return "", -1
	}

	switch p.Policy {
	case "random":
		if p.rng == nil {
			p.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
		idx := p.rng.Intn(len(p.Links))
		return p.Links[idx], idx
	case "least_conn":
		if len(p.counts) != len(p.Links) {
			p.counts = make([]int, len(p.Links))
		}
		minIdx := 0
		minVal := p.counts[0]
		for i := 1; i < len(p.counts); i++ {
			if p.counts[i] < minVal {
				minVal = p.counts[i]
				minIdx = i
			}
		}
		p.counts[minIdx]++
		return p.Links[minIdx], minIdx
	default:
		idx := p.idx % len(p.Links)
		link := p.Links[idx]
		p.idx = (p.idx + 1) % len(p.Links)
		return link, idx
	}
}

func (p *Pool) Size() int {
	return len(p.Links)
}
