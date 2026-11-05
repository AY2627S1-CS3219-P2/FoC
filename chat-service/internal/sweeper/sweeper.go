package sweeper

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"foc/chat-service/internal/chat"
)

type Sweeper struct {
	service  *chat.Service
	interval time.Duration
	log      *slog.Logger
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewSweeper(service *chat.Service, interval time.Duration, log *slog.Logger) *Sweeper {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Sweeper{
		service:  service,
		interval: interval,
		log:      log,
		stopCh:   make(chan struct{}),
	}
}

func (s *Sweeper) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Run immediately once on startup
		s.sweep(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *Sweeper) sweep(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	affected, err := s.service.FreezeExpiredCompletedConversations(sweepCtx)
	if err != nil {
		// Shutting down cancels a sweep in flight; that is not a failure.
		if ctx.Err() == nil {
			s.log.Error("expiry sweeper error", "err", err)
		}
		return
	}
	if affected > 0 {
		s.log.Info("expiry sweeper froze completed conversations past 24h cutoff", "count", affected)
	}
}

func (s *Sweeper) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}
