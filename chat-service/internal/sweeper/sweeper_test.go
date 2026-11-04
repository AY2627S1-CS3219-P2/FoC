package sweeper

import (
	"context"
	"testing"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/stretchr/testify/assert"
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
	sw := NewSweeper(svc, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sw.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	sw.Stop()

	assert.True(t, repo.freezeCalled)
}
