package storage

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestDecodeCursorValidatesInternalIDAndSingleDocument(t *testing.T) {
	t.Parallel()
	cursor := Cursor{
		Watermark: 1, OccurredAt: time.Date(2026, 7, 29, 6, 0, 0, 0, time.UTC),
		ID: "00000000-0000-0000-0000-000000000001", Source: model.SourceAgentGateway,
	}
	encoded, err := EncodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeCursor(encoded, model.SourceAgentGateway); err != nil || decoded.ID != cursor.ID {
		t.Fatalf("valid cursor = %#v, %v", decoded, err)
	}

	for name, document := range map[string]string{
		"non UUID identifier": `{"v":1,"watermark":1,"occurredAt":"2026-07-29T06:00:00Z","id":"not-a-uuid","source":"agentgateway"}`,
		"trailing document":   `{"v":1,"watermark":1,"occurredAt":"2026-07-29T06:00:00Z","id":"00000000000000000000000000000001","source":"agentgateway"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			value := base64.RawURLEncoding.EncodeToString([]byte(document))
			if _, err := DecodeCursor(value, model.SourceAgentGateway); err != ErrInvalidCursor {
				t.Fatalf("DecodeCursor error = %v", err)
			}
		})
	}
}
