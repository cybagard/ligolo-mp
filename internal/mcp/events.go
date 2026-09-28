package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// eventsURI is the MCP resource URI backed by the recent-activity ring buffer.
const eventsURI = "ligolo://events/recent"

// eventTypeName maps the server's numeric event type to a label. The values
// mirror internal/events.EventType (OK=0, ERROR=1, WARNING=2); the label is
// derived here so the MCP server does not depend on the server-side (display)
// mapping.
func eventTypeName(t int32) string {
	switch t {
	case 0:
		return "INFO"
	case 1:
		return "ERROR"
	case 2:
		return "WARNING"
	default:
		return "UNKNOWN"
	}
}

// recentEvent is one activity record shaped for LLM readability.
type recentEvent struct {
	Time string `json:"time"`
	Type string `json:"type"`
	Data string `json:"data"`
}

// eventBuffer is a fixed-size ring of the most recent server activity events,
// filled from the Join stream. It gives an agent recent context ("X started
// relay to Y") without holding a long-lived streaming tool call open.
type eventBuffer struct {
	mu   sync.Mutex
	buf  []recentEvent
	size int
}

func newEventBuffer(size int) *eventBuffer {
	if size <= 0 {
		size = 200
	}
	return &eventBuffer{size: size}
}

func (b *eventBuffer) add(ev *pb.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, recentEvent{
		Time: time.Now().UTC().Format(time.RFC3339),
		Type: eventTypeName(ev.GetType()),
		Data: ev.GetData(),
	})
	if len(b.buf) > b.size {
		b.buf = b.buf[len(b.buf)-b.size:]
	}
}

// snapshot returns a copy of the buffered events, oldest first.
func (b *eventBuffer) snapshot() []recentEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]recentEvent, len(b.buf))
	copy(out, b.buf)
	return out
}

// consumeEvents subscribes to the server's Join event stream and fills the ring
// buffer, transparently re-subscribing with capped backoff on stream errors. It
// runs until ctx is cancelled.
func (c *Client) consumeEvents(ctx context.Context) {
	const (
		minBackoff = 1 * time.Second
		maxBackoff = 30 * time.Second
	)
	backoff := minBackoff

	for ctx.Err() == nil {
		stream, err := c.ligolo.Join(ctx, &pb.Empty{})
		if err != nil {
			slog.Debug("event stream subscribe failed, retrying", slog.Any("error", err), slog.Duration("backoff", backoff))
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		backoff = minBackoff
		for {
			ev, err := stream.Recv()
			if err != nil {
				slog.Debug("event stream closed, will re-subscribe", slog.Any("error", err))
				break
			}
			c.events.add(ev)
		}

		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

// sleepCtx sleeps for d or until ctx is cancelled; it returns false if ctx was
// cancelled (the caller should stop).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// eventsResourceText renders the current buffer as pretty JSON for the resource.
func (c *Client) eventsResourceText() (string, error) {
	events := c.events.snapshot()
	out, err := json.MarshalIndent(map[string]any{
		"count":  len(events),
		"events": events,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
