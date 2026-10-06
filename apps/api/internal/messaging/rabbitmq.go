package messaging

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"net"
	"sync"
	"time"
)

const Exchange = "reviewpilot.tasks.v1"
const ReviewQueue = "reviewpilot.review.v1"
const EvolutionQueue = "reviewpilot.evolution.v1"
const QuarantineQueue = "reviewpilot.quarantine.v1"

// One in-flight publication per channel associates basic.return with its confirm.
type Rabbit struct {
	URL     string
	mu      sync.Mutex
	conn    *amqp.Connection
	channel *amqp.Channel
	returns chan amqp.Return
}

func NewRabbit(url string) *Rabbit { return &Rabbit{URL: url} }

type boundedConn struct{ net.Conn }

func (c boundedConn) Write(body []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(body)
}
func dial(network, address string) (net.Conn, error) {
	conn, err := amqp.DefaultDial(5*time.Second)(network, address)
	if err != nil {
		return nil, err
	}
	return boundedConn{conn}, nil
}
func Topology(channel *amqp.Channel) error {
	if err := channel.ExchangeDeclare(Exchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	for queue, key := range map[string]string{ReviewQueue: "connection-check", EvolutionQueue: "evolution"} {
		if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
			return err
		}
		if err := channel.QueueBind(queue, key, Exchange, false, nil); err != nil {
			return err
		}
	}
	_, err := channel.QueueDeclare(QuarantineQueue, true, false, false, false, nil)
	return err
}
func (r *Rabbit) connect() error {
	if r.channel != nil && !r.channel.IsClosed() {
		return nil
	}
	r.reset()
	conn, err := amqp.DialConfig(r.URL, amqp.Config{Heartbeat: 10 * time.Second, Dial: dial})
	if err != nil {
		return errors.New("BROKER_UNAVAILABLE")
	}
	r.conn = conn
	r.channel, err = conn.Channel()
	if err != nil {
		r.reset()
		return errors.New("BROKER_CHANNEL_UNAVAILABLE")
	}
	if err = Topology(r.channel); err == nil {
		err = r.channel.Confirm(false)
	}
	if err != nil {
		r.reset()
		return errors.New("BROKER_TOPOLOGY_UNAVAILABLE")
	}
	r.returns = r.channel.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}
func (r *Rabbit) reset() {
	if r.conn != nil {
		_ = r.conn.CloseDeadline(time.Now().Add(time.Second))
	}
	r.channel = nil
	r.conn = nil
	r.returns = nil
}
func (r *Rabbit) Close() { r.mu.Lock(); defer r.mu.Unlock(); r.reset() }
func (r *Rabbit) Publish(ctx context.Context, key, messageID string, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.connect(); err != nil {
		return err
	}
	confirmed, err := r.channel.PublishWithDeferredConfirmWithContext(ctx, Exchange, key, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: messageID, Body: body})
	if err != nil {
		r.reset()
		return errors.New("BROKER_PUBLISH_FAILED")
	}
	ok, err := confirmed.WaitContext(ctx)
	if err != nil || !ok {
		r.reset()
		return errors.New("BROKER_CONFIRM_FAILED")
	}
	// RabbitMQ sends return before ack; amqp091 dispatches both on the same reader.
	select {
	case <-r.returns:
		r.reset()
		return errors.New("BROKER_UNROUTABLE")
	default:
		return nil
	}
}
