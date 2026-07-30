package shared

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeCursor(t *testing.T) {
	createdAt := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	id := "550e8400-e29b-41d4-a716-446655440000"

	cursor, err := EncodeCursor(createdAt, id)
	require.NoError(t, err)

	decoded, err := DecodeCursor(cursor)
	require.NoError(t, err)
	assert.Equal(t, createdAt, decoded.CreatedAt)
	assert.Equal(t, id, decoded.ID)
}

func TestDecodeCursor_Malformed(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"not base64", "not-valid-base64!!!"},
		{"valid base64 but not JSON", "dGhpcyBpcyBub3QganNvbg=="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeCursor(tt.cursor)
			assert.Error(t, err)
			assert.Equal(t, "invalid cursor", err.Error())
		})
	}
}

func TestDecodeCursor_EmptyString(t *testing.T) {
	cursor, err := DecodeCursor("")
	assert.Nil(t, cursor)
	assert.NoError(t, err)
}

func TestNormalizeLimit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string defaults to 20", "", 20},
		{"non-numeric defaults to 20", "abc", 20},
		{"zero defaults to 20", "0", 20},
		{"negative defaults to 20", "-5", 20},
		{"valid value", "50", 50},
		{"clamped to max 100", "999", 100},
		{"max boundary", "100", 100},
		{"just under max", "99", 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeLimit(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateCursor(t *testing.T) {
	tests := []struct {
		name    string
		cursor  *Cursor
		wantErr bool
	}{
		{"nil cursor is valid", nil, false},
		{"valid cursor", &Cursor{CreatedAt: time.Now(), ID: "550e8400-e29b-41d4-a716-446655440000"}, false},
		{"zero created_at is invalid", &Cursor{CreatedAt: time.Time{}, ID: "550e8400-e29b-41d4-a716-446655440000"}, true},
		{"empty id is invalid", &Cursor{CreatedAt: time.Now(), ID: ""}, true},
		{"invalid uuid is invalid", &Cursor{CreatedAt: time.Now(), ID: "not-a-uuid"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCursor(tt.cursor)
			if tt.wantErr {
				assert.Equal(t, ErrInvalidCursor, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
