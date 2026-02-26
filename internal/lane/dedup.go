package lane

import (
	"github.com/dgraph-io/ristretto"
)

type DedupCache struct {
	local *ristretto.Cache
	redis any
}

func NewDedupCache(redisClient any) *DedupCache {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})

	return &DedupCache{
		local: cache,
		redis: redisClient,
	}
}

func (d *DedupCache) Local() *ristretto.Cache {
	return d.local
}
