package lane

import (
	"time"

	"github.com/dgraph-io/ristretto"
)

type CachedResponse struct {
	MessageID  string
	RunID      string
	SessionID  string
	Content    []byte
	FinishedAt time.Time
}

type Dedup struct {
	local *ristretto.Cache
	redis any //nolint:unused // TODO: Redis integration for persistent cache
}

func NewDedup() *Dedup {
	cache, _ := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})

	return &Dedup{
		local: cache,
	}
}

func (d *Dedup) StoreResponse(sessionID, messageID string, response []byte) error {
	key := sessionID + ":" + messageID
	d.local.Set(key, response, int64(len(response)))
	d.local.Wait()
	return nil
}

func (d *Dedup) GetResponse(sessionID, messageID string) []byte {
	key := sessionID + ":" + messageID
	if value, found := d.local.Get(key); found {
		if response, ok := value.([]byte); ok {
			return response
		}
	}
	return nil
}
