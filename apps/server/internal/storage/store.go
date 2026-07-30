// Package storage defines the persistence contracts used by Audit ingest and
// resumable event delivery.
package storage

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

var (
	ErrNotFound      = errors.New("stored audit record was not found")
	ErrInvalidCursor = errors.New("invalid audit cursor")
)

type EventFilter struct {
	Source model.Source
	Cursor string
	Limit  int
}

type Page[T any] struct {
	Items      []T
	NextCursor *string
	Total      int
}

type PersistResult struct {
	EventID        string
	Changed        bool
	OutboxSequence int64
}

type Checkpoint struct {
	Source        string
	Cursor        json.RawMessage
	LastSuccessAt *time.Time
	LastAttemptAt *time.Time
	LastError     string
	UpdatedAt     time.Time
}

type AuditPayload struct {
	EventID        string
	ContentType    string
	Encoding       string
	PayloadBytes   []byte
	PayloadJSON    json.RawMessage
	RedactionState string
	SizeBytes      int64
	ExpiresAt      *time.Time
	CreatedAt      time.Time
}

type OutboxMessage struct {
	Sequence  int64
	Topic     string
	EntityID  string
	EventKind string
	Event     model.UnifiedEvent
	Trace     *telemetry.Summary
	Demo      *model.DemoRunEvent
	CreatedAt time.Time
	ExpiresAt *time.Time
}

type ReplayBatch struct {
	Messages []OutboxMessage
	Oldest   int64
	Latest   int64
}

// AuditEventWriter atomically persists normalized event changes, their outbox
// messages, optional payloads, and the source checkpoint.
type AuditEventWriter interface {
	PersistEvents(context.Context, []model.UnifiedEvent, *Checkpoint) ([]PersistResult, error)
}

type AuditEventStore interface {
	ListEvents(context.Context, EventFilter) (Page[model.UnifiedEvent], error)
	GetEvent(context.Context, model.Source, string) (model.UnifiedEvent, error)
}

type PayloadStore interface {
	PutPayload(context.Context, AuditPayload) error
	GetPayload(context.Context, string) (AuditPayload, error)
}

type CheckpointStore interface {
	GetCheckpoint(context.Context, string) (Checkpoint, error)
	SaveCheckpoint(context.Context, Checkpoint) error
}

type OutboxStore interface {
	ReplayAfter(context.Context, int64, int) (ReplayBatch, error)
}

type Readiness interface {
	Ready(context.Context) error
}

type Migrator interface {
	Migrate(context.Context) error
}

type Maintainer interface {
	Prune(context.Context, time.Time) error
}

type AuditStore interface {
	AuditEventWriter
	AuditEventStore
	PayloadStore
	CheckpointStore
	OutboxStore
	Readiness
	Maintainer
}

type Cursor struct {
	Version    int          `json:"v"`
	Watermark  int64        `json:"watermark"`
	OccurredAt time.Time    `json:"occurredAt"`
	ID         string       `json:"id"`
	Source     model.Source `json:"source,omitempty"`
}

func EncodeCursor(cursor Cursor) (string, error) {
	cursor.Version = 1
	document, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(document), nil
}

func DecodeCursor(value string, source model.Source) (Cursor, error) {
	document, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var cursor Cursor
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Watermark < 1 ||
		cursor.OccurredAt.IsZero() || !validCursorID(cursor.ID) || cursor.Source != source {
		return Cursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Cursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func validCursorID(value string) bool {
	normalized := value
	switch len(value) {
	case 32:
	case 36:
		for _, index := range []int{8, 13, 18, 23} {
			if value[index] != '-' {
				return false
			}
		}
		normalized = strings.ReplaceAll(value, "-", "")
	default:
		return false
	}
	_, err := hex.DecodeString(normalized)
	return err == nil
}

func NormalizeLimit(limit int) int {
	if limit < 1 {
		return 25
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func SummaryEvent(event model.UnifiedEvent) model.UnifiedEvent {
	event.Raw = nil
	return event
}

func EventIdentity(event model.UnifiedEvent) (string, error) {
	if event.Source == "" || strings.TrimSpace(event.ID) == "" {
		return "", fmt.Errorf("audit event source and ID are required")
	}
	upstreamID := strings.TrimSpace(event.RawRef.ID)
	if upstreamID == "" {
		return "", fmt.Errorf("audit event upstream ID is required")
	}
	if event.Timestamp.IsZero() {
		return "", fmt.Errorf("audit event timestamp is required")
	}
	return string(event.Source) + "\x00" + upstreamID, nil
}
