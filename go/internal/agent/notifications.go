package agent

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/notify"
)

type notificationSubscriber struct {
	channel string
	events  chan notify.Notification
}

type notificationFanout struct {
	pool   *pgxpool.Pool
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	next   uint64
	closed bool
	subs   map[uint64]notificationSubscriber
}

var processNotificationFanouts = struct {
	sync.Mutex
	byPool map[*pgxpool.Pool]*notificationFanout
}{byPool: make(map[*pgxpool.Pool]*notificationFanout)}

// subscribeAgentNotifications 让同一 pgxpool 上的 Turn / Control SSE 共用一条 PostgreSQL LISTEN。
//
// StreamTurnEvents 与控制面 SSE 调用。通知只是唤醒；journal 轮询才是不丢事件的回落。订阅者慢或 listen 退出时，调用方必须继续按游标读 agent_turn_events。
//
// pool 为 nil 返回空订阅。禁区：不要把 NOTIFY payload 当成 journal 内容；不要在 fanout 里写业务表。
func subscribeAgentNotifications(pool *pgxpool.Pool, channel string) (<-chan notify.Notification, func()) {
	if pool == nil {
		return nil, func() {}
	}
	processNotificationFanouts.Lock()
	hub := processNotificationFanouts.byPool[pool]
	if hub == nil {
		ctx, cancel := context.WithCancel(context.Background())
		hub = &notificationFanout{pool: pool, ctx: ctx, cancel: cancel, subs: make(map[uint64]notificationSubscriber)}
		processNotificationFanouts.byPool[pool] = hub
		go hub.listen()
	}
	processNotificationFanouts.Unlock()
	return hub.subscribe(channel)
}

// subscribe 登记一个有界缓冲的订阅。hub 已关闭则立刻返回关闭的 channel。
//
// 最后一个订阅退出会 cancel listen。缓冲满时 publish 丢弃该条（唤醒丢失），调用方靠 journal 轮询补齐。
func (h *notificationFanout) subscribe(channel string) (<-chan notify.Notification, func()) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		closed := make(chan notify.Notification)
		close(closed)
		return closed, func() {}
	}
	id := h.next
	h.next++
	events := make(chan notify.Notification, 32)
	h.subs[id] = notificationSubscriber{channel: channel, events: events}
	h.mu.Unlock()
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
	}
}

func (h *notificationFanout) listen() {
	notes, err := notify.Listen(h.ctx, h.pool, notify.ChannelTurn, notify.ChannelControl)
	if err == nil {
		for note := range notes {
			h.publish(note)
		}
	}
	h.stop()
}

func (h *notificationFanout) publish(note notify.Notification) {
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

func (h *notificationFanout) stop() {
	processNotificationFanouts.Lock()
	if processNotificationFanouts.byPool[h.pool] == h {
		delete(processNotificationFanouts.byPool, h.pool)
	}
	processNotificationFanouts.Unlock()

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
