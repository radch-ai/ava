package zitadel

import (
	"context"

	"github.com/rs/zerolog"
)

// Service handles Zitadel webhook business logic.
type Service struct {
	store  Store
	logger zerolog.Logger
}

// NewService creates a new Zitadel webhook service.
func NewService(store Store, logger zerolog.Logger) *Service {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Service{
		store:  store,
		logger: logger,
	}
}

// CreateUser creates a user record. Idempotent - safe if user already exists.
func (s *Service) CreateUser(ctx context.Context, userID string) error {
	return s.store.CreateUser(ctx, userID)
}

// DeleteUser deletes a user record. CASCADE removes all related data.
// Idempotent - returns nil even if user didn't exist.
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.store.DeleteUser(ctx, userID)
}
