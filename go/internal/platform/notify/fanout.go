package notify

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type fanoutSubscriber struct {
	channel string
	events  chan Notification
}

type fanout struct {
	pool   *pgxpool.Pool
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	next   uint64
	closed bool
	subs   map[uint64]fanoutSubscriber
}

var processFanouts = struct {
	sync.Mutex
	byPool map[*pgxpool.Pool]*fanout
}{byPool: make(map[*pgxpool.Pool]*fanout)}

// Subscribe 在同一个 pgx pool 内共享一条 PostgreSQL LISTEN 连接，并按 channel 分发通知。
// 通知只是唤醒；调用方仍须按自己的游标/状态回读 PostgreSQL。缓冲满时丢弃通知，避免慢订阅者阻塞其它 SSE。
// pool 为 nil 或 channel 非法时返回 nil channel，调用方应走轮询回落。
func Subscribe(pool *pgxpool.Pool, channel string) (<-chan Notification, func()) {
	if pool == nil || !ValidChannel(channel) {
		return nil, func() {}
	}
	for {
		processFanouts.Lock()
		hub := processFanouts.byPool[pool]
		if hub == nil {
			ctx, cancel := context.WithCancel(context.Background())
			hub = &fanout{pool: pool, ctx: ctx, cancel: cancel, subs: make(map[uint64]fanoutSubscriber)}
			processFanouts.byPool[pool] = hub
			go hub.listen()
		}
		processFanouts.Unlock()
		if events, unsubscribe, ok := hub.subscribe(channel); ok {
			return events, unsubscribe
		}
	}
}

func (h *fanout) subscribe(channel string) (<-chan Notification, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, false
	}
	id := h.next
	h.next++
	events := make(chan Notification, 32)
	h.subs[id] = fanoutSubscriber{channel: channel, events: events}
	var once sync.Once
	return events, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			idle := len(h.subs) == 0
			h.mu.Unlock()
			if idle {
				h.cancel()
			}
		})
	}, true
}

func (h *fanout) listen() {
	notes, err := Listen(h.ctx, h.pool, ChannelControl, ChannelRun, ChannelTurn, ChannelImageSession)
	if err == nil {
		for note := range notes {
			h.publish(note)
		}
	}
	h.stop()
}

func (h *fanout) publish(note Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subscriber := range h.subs {
		if subscriber.channel != note.Channel {
			continue
		}
		select {
		case subscriber.events <- note:
		default:
		}
	}
}

func (h *fanout) stop() {
	processFanouts.Lock()
	if processFanouts.byPool[h.pool] == h {
		delete(processFanouts.byPool, h.pool)
	}
	processFanouts.Unlock()

	h.mu.Lock()
	if !h.closed {
		h.closed = true
		for id, subscriber := range h.subs {
			close(subscriber.events)
			delete(h.subs, id)
		}
	}
	h.mu.Unlock()
}
