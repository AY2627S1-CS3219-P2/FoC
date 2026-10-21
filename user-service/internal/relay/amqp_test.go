// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Unit tests of the RabbitMQ publisher's connection handling that need
//   no broker (lazy dialling, the dial throttle, Close, and its helpers),
//   adapted from order-service/internal/relay to the standard testing package.
// Author review: James Teo — reviewed and verified.

package relay

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// fakeClock is a settable clock for the publisher's dial throttle.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// unreachablePublisher returns a publisher whose every dial fails with
// dialErr, and a counter of its dials.
func unreachablePublisher(clock *fakeClock, dialErr error) (*AMQPPublisher, func() int) {
	var mu sync.Mutex
	dials := 0
	p := NewAMQPPublisher("amqp://foc:s3cret-password@127.0.0.1:5672/foc", "foc.events", nil)
	p.now = clock.Now
	p.dial = func(string) (*amqp.Connection, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		return nil, dialErr
	}
	return p, func() int {
		mu.Lock()
		defer mu.Unlock()
		return dials
	}
}

func TestAMQPPublisherDialsLazilyAndAtMostOncePerInterval(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	dialErr := errors.New("dial tcp 127.0.0.1:5672: connect: connection refused")
	p, dials := unreachablePublisher(clock, dialErr)
	if dials() != 0 {
		t.Fatal("constructing the publisher must not dial: the broker is not on the startup path")
	}

	steps := []struct {
		name      string
		advance   time.Duration
		wantDials int
		wantText  string
	}{
		{name: "the first publish dials", advance: 0, wantDials: 1, wantText: "connect to RabbitMQ"},
		{name: "a publish inside the interval fails without dialling", advance: redialInterval - time.Millisecond, wantDials: 1, wantText: "next dial in 1ms"},
		{name: "a publish once the interval has passed dials again", advance: time.Millisecond, wantDials: 2, wantText: "connect to RabbitMQ"},
		{name: "the interval restarts from the latest dial", advance: redialInterval / 2, wantDials: 2, wantText: "next dial in 1s"},
	}
	for _, step := range steps {
		clock.Advance(step.advance)
		err := p.Publish(context.Background(), newRecord("user.registered"))
		if err == nil {
			t.Fatalf("%s: Publish succeeded against an unreachable broker", step.name)
		}
		if !errors.Is(err, dialErr) {
			t.Errorf("%s: the error must carry the dial failure: got %v", step.name, err)
		}
		if !strings.Contains(err.Error(), step.wantText) {
			t.Errorf("%s: error %q does not contain %q", step.name, err, step.wantText)
		}
		if strings.Contains(err.Error(), "s3cret-password") {
			t.Errorf("%s: the broker URL's password leaked into %q", step.name, err)
		}
		if got := dials(); got != step.wantDials {
			t.Errorf("%s: dials = %d, want %d", step.name, got, step.wantDials)
		}
	}
}

func TestAMQPPublisherFailsWithoutDiallingWhenItCannotPublish(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		prepare func(t *testing.T, p *AMQPPublisher)
		ctx     context.Context
		wantErr error
	}{
		{
			name: "after Close",
			prepare: func(t *testing.T, p *AMQPPublisher) {
				if err := p.Close(); err != nil {
					t.Fatalf("closing a publisher that never connected: %v", err)
				}
			},
			ctx:     context.Background(),
			wantErr: ErrPublisherClosed,
		},
		{
			name:    "with a context already done",
			prepare: func(*testing.T, *AMQPPublisher) {},
			ctx:     cancelled,
			wantErr: context.Canceled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
			p, dials := unreachablePublisher(clock, errors.New("connection refused"))
			tc.prepare(t, p)
			if err := p.Publish(tc.ctx, newRecord("user.registered")); !errors.Is(err, tc.wantErr) {
				t.Errorf("Publish error = %v, want %v", err, tc.wantErr)
			}
			if got := dials(); got != 0 {
				t.Errorf("dials = %d, want 0", got)
			}
		})
	}
}

// confirmationFunc adapts a function to the confirm awaitConfirm waits on.
type confirmationFunc func(ctx context.Context) (bool, error)

func (f confirmationFunc) WaitContext(ctx context.Context) (bool, error) { return f(ctx) }

func TestAwaitConfirmOutlivesACancelledContext(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	var waitWasDone, waitHadDeadline bool
	var waitDeadline time.Time
	acked, err := awaitConfirm(cancelled, confirmationFunc(func(wctx context.Context) (bool, error) {
		select {
		case <-wctx.Done():
			waitWasDone = true
		default:
		}
		waitDeadline, waitHadDeadline = wctx.Deadline()
		return true, nil
	}))

	if err != nil {
		t.Fatalf("awaitConfirm error = %v, want nil", err)
	}
	if !acked {
		t.Error("an ack the broker sent must reach the caller")
	}
	if waitWasDone {
		t.Error("a message already sent must have its confirm awaited even though the relay is stopping")
	}
	if !waitHadDeadline {
		t.Fatal("the wait must stay bounded")
	}
	if until := time.Until(waitDeadline); until > confirmTimeout {
		t.Errorf("the wait's deadline is %s away, want at most %s", until, confirmTimeout)
	}
}

func TestClosedWith(t *testing.T) {
	withReason := make(chan *amqp.Error, 1)
	withReason <- &amqp.Error{Code: amqp.NotFound, Reason: "NOT_FOUND - no exchange 'foc.events' in vhost 'foc'"}
	graceful := make(chan *amqp.Error)
	close(graceful)

	tests := []struct {
		name       string
		c          chan *amqp.Error
		wantGone   bool
		wantReason string
	}{
		{name: "nothing open", c: nil, wantGone: false},
		{name: "still open", c: make(chan *amqp.Error, 1), wantGone: false},
		{name: "closed by the broker", c: withReason, wantGone: true, wantReason: "NOT_FOUND - no exchange"},
		{name: "closed gracefully", c: graceful, wantGone: true, wantReason: "closed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, gone := closedWith(tc.c)
			if gone != tc.wantGone {
				t.Errorf("gone = %t, want %t", gone, tc.wantGone)
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason %q does not contain %q", reason, tc.wantReason)
			}
		})
	}
}

func TestReturnedMatchesTheMessageID(t *testing.T) {
	const msgID = "8a1f3c2e-0000-4000-8000-000000000001"
	closedReturns := make(chan amqp.Return)
	close(closedReturns)

	tests := []struct {
		name    string
		returns func() chan amqp.Return
		want    bool
	}{
		{name: "no channel open", returns: func() chan amqp.Return { return nil }, want: false},
		{name: "nothing returned", returns: func() chan amqp.Return { return make(chan amqp.Return, 1) }, want: false},
		{
			name: "this message returned",
			returns: func() chan amqp.Return {
				c := make(chan amqp.Return, 1)
				c <- amqp.Return{MessageId: msgID, ReplyCode: amqp.NoRoute, ReplyText: "NO_ROUTE"}
				return c
			},
			want: true,
		},
		{
			name: "only another message returned",
			returns: func() chan amqp.Return {
				c := make(chan amqp.Return, 1)
				c <- amqp.Return{MessageId: "another", ReplyCode: amqp.NoRoute}
				return c
			},
			want: false,
		},
		{name: "channel closed", returns: func() chan amqp.Return { return closedReturns }, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &AMQPPublisher{returns: tc.returns()}
			ret, ok := p.returned(msgID)
			if ok != tc.want {
				t.Fatalf("returned = %t, want %t", ok, tc.want)
			}
			if tc.want && ret.ReplyCode != uint16(amqp.NoRoute) {
				t.Errorf("ReplyCode = %d, want %d", ret.ReplyCode, amqp.NoRoute)
			}
		})
	}
}
