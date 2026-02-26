package agent

import (
	"sync"

	"github.com/google/uuid"
)

type Task struct {
	ID        string
	Message   string
	CreatedAt int64
}

type Lane struct {
	pending []*Task
	steerCh chan struct{}
	mu      sync.Mutex
}

func NewLane() *Lane {
	return &Lane{
		pending: []*Task{},
		steerCh: make(chan struct{}, 1),
	}
}

func (l *Lane) SteerCh() <-chan struct{} {
	return l.steerCh
}

func (l *Lane) SignalSteer() {
	select {
	case l.steerCh <- struct{}{}:
	default:
	}
}

func (l *Lane) Pending() []*Task {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := make([]*Task, len(l.pending))
	copy(result, l.pending)
	return result
}

func (l *Lane) Submit(message string) string {
	id := uuid.New().String()
	task := &Task{
		ID:        id,
		Message:   message,
		CreatedAt: 0,
	}

	l.mu.Lock()
	l.pending = append(l.pending, task)
	l.mu.Unlock()

	l.SignalSteer()
	return id
}
