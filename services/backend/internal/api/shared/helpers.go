package shared

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var ErrInvalidCursor = errors.New("invalid cursor")

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func ParseUserID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func PgBool(v bool) pgtype.Bool {
	return pgtype.Bool{Bool: v, Valid: true}
}

func PgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func UUIDFromString(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func EncodeCursor(createdAt time.Time, id string) (string, error) {
	cursor := Cursor{CreatedAt: createdAt, ID: id}
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func DecodeCursor(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var cursor Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, ErrInvalidCursor
	}
	return &cursor, nil
}

func NormalizeLimit(raw string) int {
	const defaultLimit = 20
	const maxLimit = 100

	if raw == "" {
		return defaultLimit
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return defaultLimit
	}
	if val <= 0 {
		return defaultLimit
	}
	if val > maxLimit {
		return maxLimit
	}
	return val
}

// ValidateCursor validates that a cursor has valid CreatedAt and ID fields.
// Returns nil if cursor is nil or valid.
func ValidateCursor(cursor *Cursor) error {
	if cursor == nil {
		return nil
	}
	if cursor.CreatedAt.IsZero() {
		return ErrInvalidCursor
	}
	if cursor.ID == "" {
		return ErrInvalidCursor
	}
	var uuid pgtype.UUID
	if err := uuid.Scan(cursor.ID); err != nil {
		return ErrInvalidCursor
	}
	return nil
}
