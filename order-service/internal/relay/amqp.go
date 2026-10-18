// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: The relay's RabbitMQ publisher: lazy, throttled (re)connection,
//   publisher confirms and mandatory routing to foc.events (D-051, NFR5.4).
// Author review: James Teo — reviewed and verified.

package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// redialInterval spaces dial attempts, so while the broker is down a
	// publish fails at once instead of each one waiting on a dial.
	redialInterval = 2 * time.Second
	// dialTimeout bounds one dial, TCP connect and AMQP handshake together.
	dialTimeout = 5 * time.Second
	// confirmTimeout bounds the wait for the broker's confirm of one publish.
	confirmTimeout = 5 * time.Second
	// closeTimeout bounds closing a connection the broker may not answer on.
	closeTimeout = 2 * time.Second
)

// ErrPublisherClosed is returned by Publish after Close.
var ErrPublisherClosed = errors.New("relay: publisher is closed")

// AMQPPublisher is the Publisher for RabbitMQ (D-051). It publishes to one
// exchange with publisher confirms and the mandatory flag, so a record counts
// as sent only once the broker has routed it to a queue. It connects on the
// first Publish rather than at construction, so the broker is never on
// order-service's startup path, and reconnects after the broker closes the
// connection or the channel. Publishes are serialized; it is safe for
// concurrent use.
type AMQPPublisher struct {
	url      string
	exchange string
	log      *slog.Logger
	// dial and now are fields so tests can replace them.
	dial func(url string) (*amqp.Connection, error)
	now  func() time.Time

	mu          sync.Mutex
	conn        *amqp.Connection
	connClosed  chan *amqp.Error
	ch          *amqp.Channel
	chClosed    chan *amqp.Error
	returns     chan amqp.Return
	lastDial    time.Time
	lastDialErr error
	closed      bool
}

// NewAMQPPublisher returns a publisher to exchange on the broker at url. It
// does not connect. A nil log discards the publisher's logs.
func NewAMQPPublisher(url, exchange string, log *slog.Logger) *AMQPPublisher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &AMQPPublisher{url: url, exchange: exchange, log: log, dial: dialAMQP, now: time.Now}
}

// dialAMQP replaces the library's default 30-second dial and handshake limit
// with dialTimeout, so a broker that never answers holds up a publish, and the
// outbox row it has locked, for less time.
func dialAMQP(url string) (*amqp.Connection, error) {
	return amqp.DialConfig(url, amqp.Config{Dial: amqp.DefaultDial(dialTimeout)})
}

// Publish sends r to the exchange under r.RoutingKey and waits for the
// broker's confirm. It fails if the broker cannot be reached, does not
// confirm within confirmTimeout, nacks the message, or returns it as
// unroutable because no queue is bound to the key. Once r is sent, ctx ending
// does not cut short the wait for its confirm (see awaitConfirm).
func (p *AMQPPublisher) Publish(ctx context.Context, r Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPublisherClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ch, err := p.channel(ctx)
	if err != nil {
		return err
	}

	msgID := r.EventID.String()
	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, p.exchange, r.RoutingKey, true, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     msgID,
		CorrelationId: r.CorrelationID,
		Type:          r.RoutingKey,
		Timestamp:     p.now(),
		Body:          r.Payload,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		p.dropConnection()
		return fmt.Errorf("publish event %s to RabbitMQ: %w", msgID, err)
	}

	acked, err := awaitConfirm(ctx, dc)
	if err != nil {
		// The connection may be wedged, and a confirm or return that arrives
		// late must not be taken for a later publish's: start afresh.
		p.dropConnection()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("RabbitMQ did not confirm event %s within %s", msgID, confirmTimeout)
	}
	if ret, ok := p.returned(msgID); ok {
		return fmt.Errorf("RabbitMQ could not route event %s: no queue is bound to %q on exchange %q (%d %s)",
			msgID, r.RoutingKey, p.exchange, ret.ReplyCode, ret.ReplyText)
	}
	if !acked {
		// Pending confirms are nacked when the channel closes: report why.
		if reason, gone := closedWith(p.chClosed); gone {
			return fmt.Errorf("RabbitMQ closed the channel before confirming event %s: %s", msgID, reason)
		}
		return fmt.Errorf("RabbitMQ refused event %s (nack)", msgID)
	}
	return nil
}

// confirmation is the broker's pending confirm of one publish: the method of
// *amqp.DeferredConfirmation that awaitConfirm uses, so a test can stand in.
type confirmation interface {
	WaitContext(ctx context.Context) (acked bool, err error)
}

// awaitConfirm waits up to confirmTimeout for the broker's confirm of a
// message already sent, however ctx ends meanwhile. Once
// PublishWithDeferredConfirmWithContext has returned, the message is on the
// wire: the library checks ctx only before sending. Waiting on ctx itself would
// make a relay that is stopping miss an ack the broker sends, so the store
// would roll the row back and a restart would publish the event a second time.
// Waiting on a detached context lets it learn of the ack, and the store marks
// the row published.
func awaitConfirm(ctx context.Context, dc confirmation) (bool, error) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), confirmTimeout)
	defer cancel()
	return dc.WaitContext(wctx)
}

// Close closes the broker connection, if one is open. Publish fails with
// ErrPublisherClosed afterwards.
func (p *AMQPPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.conn == nil {
		return nil
	}
	err := p.conn.CloseDeadline(time.Now().Add(closeTimeout))
	p.forget()
	if err != nil && !errors.Is(err, amqp.ErrClosed) {
		return fmt.Errorf("close RabbitMQ connection: %w", err)
	}
	return nil
}

// channel returns an open channel in confirm mode, reconnecting first if the
// broker has closed the connection, or reopening the channel if only that
// closed. The caller holds p.mu.
func (p *AMQPPublisher) channel(ctx context.Context) (*amqp.Channel, error) {
	if reason, gone := closedWith(p.connClosed); gone {
		p.log.WarnContext(ctx, "RabbitMQ connection closed; redialling", "reason", reason)
		p.dropConnection()
	}
	if reason, gone := closedWith(p.chClosed); gone {
		p.log.WarnContext(ctx, "RabbitMQ channel closed; reopening", "reason", reason)
		p.ch, p.chClosed, p.returns = nil, nil, nil
	}
	if p.conn == nil {
		if err := p.connect(ctx); err != nil {
			return nil, err
		}
	}
	if p.ch == nil {
		if err := p.openChannel(); err != nil {
			return nil, err
		}
	}
	return p.ch, nil
}

// connect dials the broker, at most once per redialInterval. The dial itself
// cannot be cancelled; dialTimeout bounds it.
func (p *AMQPPublisher) connect(ctx context.Context) error {
	now := p.now()
	if since := now.Sub(p.lastDial); !p.lastDial.IsZero() && since < redialInterval {
		retryIn := (redialInterval - since).Round(time.Millisecond)
		if p.lastDialErr != nil {
			return fmt.Errorf("RabbitMQ unreachable, next dial in %s: %w", retryIn, p.lastDialErr)
		}
		return fmt.Errorf("RabbitMQ connection lost, next dial in %s", retryIn)
	}
	p.lastDial = now
	conn, err := p.dial(p.url)
	p.lastDialErr = err
	if err != nil {
		// amqp091-go's error for a URL that does not parse quotes the whole
		// URL, password included, but config.Load has already rejected any
		// RABBITMQ_URL that net/url cannot parse, so a dial error here names
		// at most the broker's host and port.
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	p.conn = conn
	p.connClosed = conn.NotifyClose(make(chan *amqp.Error, 1))
	p.log.InfoContext(ctx, "connected to RabbitMQ", "exchange", p.exchange)
	return nil
}

// openChannel opens a channel on p.conn and puts it in confirm mode. A
// connection that cannot open one is treated as broken.
func (p *AMQPPublisher) openChannel() error {
	ch, err := p.conn.Channel()
	if err != nil {
		p.dropConnection()
		return fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	chClosed := ch.NotifyClose(make(chan *amqp.Error, 1))
	// One publish is in flight at a time and each can be returned at most
	// once, so one slot never blocks the library's reader.
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	if err := ch.Confirm(false); err != nil {
		p.dropConnection()
		return fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	p.ch, p.chClosed, p.returns = ch, chClosed, returns
	return nil
}

// dropConnection closes the connection, and with it the channel, without
// waiting long on a broker that may not answer. It takes no context because
// it must finish even when the publish was cancelled; closeTimeout bounds it.
// The next Publish redials.
func (p *AMQPPublisher) dropConnection() {
	if p.conn != nil {
		_ = p.conn.CloseDeadline(time.Now().Add(closeTimeout))
	}
	p.forget()
}

// forget clears the connection state, so the next Publish starts afresh.
func (p *AMQPPublisher) forget() {
	p.conn, p.connClosed = nil, nil
	p.ch, p.chClosed, p.returns = nil, nil, nil
}

// returned reports whether the broker sent back the message msgID as
// unroutable. RabbitMQ sends basic.return before the confirm, and the library
// hands it over before resolving the confirm, so once the confirm is in, any
// return for the message is already waiting here.
func (p *AMQPPublisher) returned(msgID string) (amqp.Return, bool) {
	for {
		select {
		case ret, ok := <-p.returns:
			if !ok {
				return amqp.Return{}, false
			}
			if ret.MessageId == msgID {
				return ret, true
			}
		default:
			return amqp.Return{}, false
		}
	}
}

// closedWith reports whether a NotifyClose channel has fired, and why. A nil
// channel (nothing open) has not.
func closedWith(c chan *amqp.Error) (reason string, gone bool) {
	select {
	case e := <-c:
		if e != nil {
			return e.Error(), true
		}
		return "closed", true
	default:
		return "", false
	}
}
