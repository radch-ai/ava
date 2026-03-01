package agent

import (
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestWSConnRef_GetSetClear(t *testing.T) {
	ref := NewWSConnRef()

	assert.Nil(t, ref.Get())
	assert.False(t, ref.IsSet())

	mockConn := &websocket.Conn{}
	ref.Set(mockConn)

	assert.Equal(t, mockConn, ref.Get())
	assert.True(t, ref.IsSet())

	ref.Clear()

	assert.Nil(t, ref.Get())
	assert.False(t, ref.IsSet())
}

func TestWSConnRef_Concurrent(t *testing.T) {
	ref := NewWSConnRef()

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			ref.Set(nil)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = ref.Get()
		}
		done <- true
	}()

	<-done
	<-done
}

func TestWSConnRef_SetReplacesConnection(t *testing.T) {
	ref := NewWSConnRef()

	conn1 := &websocket.Conn{}
	conn2 := &websocket.Conn{}

	ref.Set(conn1)
	assert.Equal(t, conn1, ref.Get())

	ref.Set(conn2)
	assert.Equal(t, conn2, ref.Get())
}

func TestWSConnRef_ClearWhenNil(t *testing.T) {
	ref := NewWSConnRef()

	assert.NotPanics(t, func() {
		ref.Clear()
	})

	assert.Nil(t, ref.Get())
}

func TestWSConnRef_IsSetAfterOperations(t *testing.T) {
	ref := NewWSConnRef()

	assert.False(t, ref.IsSet())

	ref.Set(&websocket.Conn{})
	assert.True(t, ref.IsSet())

	ref.Clear()
	assert.False(t, ref.IsSet())

	ref.Set(nil)
	assert.False(t, ref.IsSet())
}
