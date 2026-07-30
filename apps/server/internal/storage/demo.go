package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrDemoRunNotFound   = errors.New("demo run was not found")
	ErrDemoRunBusy       = errors.New("a demo run is already active")
	ErrDemoRunConflict   = errors.New("demo run state changed")
	ErrInvalidDemoCursor = errors.New("invalid demo run cursor")
)

type DemoRunFilter struct {
	Cursor string
	Limit  int
}

type DemoMutation struct {
	Run             model.DemoRun
	ExpectedVersion int64
	Event           model.DemoRunEvent
}

type DemoStreamSnapshot struct {
	Run            model.DemoRun
	LatestSequence int64
}

type DemoStore interface {
	CreateDemoRun(context.Context, model.DemoRun, model.DemoRunEvent) (model.DemoRun, int64, error)
	UpdateDemoRun(context.Context, DemoMutation) (model.DemoRun, int64, error)
	GetDemoRun(context.Context, string) (model.DemoRun, error)
	GetDemoRunByRequestID(context.Context, string) (model.DemoRun, error)
	GetActiveDemoRun(context.Context) (model.DemoRun, error)
	ListDemoRuns(context.Context, DemoRunFilter) (Page[model.DemoRun], error)
	ReplayDemoAfter(context.Context, string, int64, int) (ReplayBatch, error)
	GetDemoStreamSnapshot(context.Context, string) (DemoStreamSnapshot, error)
}

type demoCursor struct {
	Version   int   `json:"v"`
	Watermark int64 `json:"watermark"`
	Sequence  int64 `json:"sequence"`
}

func EncodeDemoCursor(watermark, sequence int64) (string, error) {
	document, err := json.Marshal(demoCursor{Version: 1, Watermark: watermark, Sequence: sequence})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(document), nil
}

func DecodeDemoCursor(value string) (int64, int64, error) {
	document, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return 0, 0, ErrInvalidDemoCursor
	}
	var cursor demoCursor
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != 1 || cursor.Watermark < 1 || cursor.Sequence < 1 || cursor.Sequence > cursor.Watermark {
		return 0, 0, ErrInvalidDemoCursor
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return 0, 0, ErrInvalidDemoCursor
	}
	return cursor.Watermark, cursor.Sequence, nil
}
