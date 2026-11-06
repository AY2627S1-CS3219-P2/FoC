// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Added the table-driven test of the sweep's log lines and its stub
//   repository, and the logger argument to the existing test (D-135).
// Author review: James Teo — reviewed and verified.

package sweeper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSweepRepo struct {
	chat.Repository
	freezeCalled bool
}

func (f *fakeSweepRepo) FreezeConversationsPast24Hours(ctx context.Context, cutoff time.Time) (int64, error) {
	f.freezeCalled = true
	return 2, nil
}

func TestSweeper_Execution(t *testing.T) {
	repo := &fakeSweepRepo{}
	svc := chat.NewService(repo, nil, nil)
	sw := NewSweeper(svc, 50*time.Millisecond, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sw.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	sw.Stop()

	assert.True(t, repo.freezeCalled)
}

// stubSweepRepo answers the expiry sweep with affected and err.
type stubSweepRepo struct {
	chat.Repository
	affected int64
	err      error
}

func (s *stubSweepRepo) FreezeConversationsPast24Hours(context.Context, time.Time) (int64, error) {
	return s.affected, s.err
}

func TestSweepLogsItsOutcome(t *testing.T) {
	cases := []struct {
		name      string
		affected  int64
		err       error
		shutdown  bool   // the sweep's context is cancelled, as on shutdown
		wantLevel string // "" when nothing may be logged
		wantMsg   string
	}{
		{name: "a sweep that froze conversations", affected: 2,
			wantLevel: "INFO", wantMsg: "expiry sweeper froze completed conversations past 24h cutoff"},
		{name: "a sweep with nothing to freeze"},
		{name: "a sweep the database fails", err: errors.New("freeze past 24h: connection refused"),
			wantLevel: "ERROR", wantMsg: "expiry sweeper error"},
		{name: "a sweep cut short by shutdown", err: context.Canceled, shutdown: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			svc := chat.NewService(&stubSweepRepo{affected: tc.affected, err: tc.err}, nil, nil)
			sw := NewSweeper(svc, time.Minute, slog.New(slog.NewJSONHandler(&logs, nil)))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.shutdown {
				cancel()
			}

			sw.sweep(ctx)

			if tc.wantLevel == "" {
				assert.Empty(t, logs.String())
				return
			}
			var line map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &line), "exactly one JSON line: %s", logs.String())
			assert.Equal(t, tc.wantLevel, line["level"])
			assert.Equal(t, tc.wantMsg, line["msg"])
			if tc.err != nil {
				assert.Equal(t, tc.err.Error(), line["err"])
			} else {
				assert.EqualValues(t, tc.affected, line["count"])
			}
		})
	}
}
