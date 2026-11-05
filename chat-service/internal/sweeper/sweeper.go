package sweeper

import (
	"context"
	"log"
	"sync"
	"time"

	"foc/chat-service/internal/chat"
)

type Sweeper struct {
	service  *chat.Service
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewSweeper(service *chat.Service, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Sweeper{
		service:  service,
		interval: interval,
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
		log.Printf("expiry sweeper error: %v", err)
		return
	}
	if affected > 0 {
		log.Printf("expiry sweeper froze %d completed conversation(s) past 24h cutoff", affected)
	}
}

func (s *Sweeper) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}
