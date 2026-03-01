package agent

import (
	"sync"

	"github.com/gorilla/websocket"
)

type WSConnRef struct {
	mu   sync.RWMutex
	conn *websocket.Conn
}

func NewWSConnRef() *WSConnRef {
	return &WSConnRef{}
}

func (r *WSConnRef) Get() *websocket.Conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conn
}

func (r *WSConnRef) Set(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn = conn
}

func (r *WSConnRef) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn = nil
}

func (r *WSConnRef) IsSet() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conn != nil
}
