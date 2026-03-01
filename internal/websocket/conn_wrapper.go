package websocket

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WSConnWrapper struct {
	writeCh      chan<- *wsMessage
	mu           sync.Mutex
	writeTimeout time.Duration
}

func NewWSConnWrapper(writeCh chan<- *wsMessage) *WSConnWrapper {
	return &WSConnWrapper{
		writeCh:      writeCh,
		writeTimeout: 100 * time.Millisecond,
	}
}

func (w *WSConnWrapper) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	msg := wsMsgPool.Get().(*wsMessage)
	msg.messageType = websocket.TextMessage
	msg.data = data

	select {
	case w.writeCh <- msg:
		return nil
	case <-time.After(w.writeTimeout):
		msg.messageType = 0
		msg.data = nil
		wsMsgPool.Put(msg)
		return websocket.ErrCloseSent
	}
}
