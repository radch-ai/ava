package lane

import (
	"github.com/dgraph-io/ristretto"
)

type DedupKey struct {
	TenantID  string
	UserID    string
	AgentID   string
	MessageID string
}

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

func (d *DedupCache) Check(userID, agentID, messageID string) (bool, []byte) {
	key := d.localKey(userID, agentID, messageID)

	_, found := d.local.Get(key.UserID + ":" + key.AgentID + ":" + key.MessageID)
	if found {
		return false, nil
	}

	return true, nil
}

func (d *DedupCache) Mark(userID, agentID, messageID string) {
	key := d.localKey(userID, agentID, messageID)
	d.local.Set(key.UserID+":"+key.AgentID+":"+key.MessageID, true, 1)
	d.local.Wait()
}

func (d *DedupCache) localKey(userID, agentID, messageID string) DedupKey {
	return DedupKey{
		TenantID:  "",
		UserID:    userID,
		AgentID:   agentID,
		MessageID: messageID,
	}
}
