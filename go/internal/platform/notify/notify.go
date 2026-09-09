// Package notify 用 PostgreSQL LISTEN/NOTIFY 做跨进程唤醒。
// 写路径走 GORM `pg_notify`（随事务提交投递）；读路径需要独立 pgx 连接，因为 LISTEN 绑在会话上。
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

const (
	// ChannelControl 是控制面唤醒通道。
	ChannelControl = "productflow_control"
	// ChannelRun 是工作流 Run 唤醒通道。
	ChannelRun = "productflow_run"
	// ChannelTurn 是 Agent Turn 唤醒通道。
	ChannelTurn = "productflow_turn"
	// ChannelImageSession 是连续生图会话唤醒通道。
	ChannelImageSession = "productflow_image_session"
	maxPayload          = 7900
)

// ErrNoPool 表示 Listen 收到了 nil pgx 池。调用方应改走轮询，不要 panic。
var ErrNoPool = errors.New("notify: pgx pool is nil")

// ListenerConnections 是当前进程占用的 PostgreSQL LISTEN 连接数（gauge）。
// Subscribe 按 pool 共享 fanout，正常运行时每个 API 副本每个 pool 为 0 或 1。
var ListenerConnections atomic.Int64

// Notification 是一条 LISTEN 收到的消息，不是 HTTP 体。
// Channel 必为本包闭集常量；Payload 在图/会话 SSE 里通常是聚合 id。
type Notification struct {
	Channel string // 必须是本包闭集常量
	Payload string // 通常是聚合 id；超长会按 UTF-8 截断
}

// ValidChannel 报告 name 是否为本包闭集（control/run/turn/image_session/dispatch）。
// 调用时机：Publish/Listen 入参校验。拼 SQL 标识符前还要过 safeIdent，不要只信本函数。
func ValidChannel(name string) bool {
	switch name {
	case ChannelControl, ChannelRun, ChannelTurn, ChannelImageSession:
		return true
	default:
		return false
	}
}

// Publish 在同一 GORM 事务里 SELECT pg_notify；db 为 nil 时静默成功。
// payload 超过 maxPayload 会按 UTF-8 边界截断。非法 channel 或非 UTF-8 返回 error。
func Publish(ctx context.Context, db *gorm.DB, channel, payload string) error {
	if db == nil {
		return nil
	}
	if !ValidChannel(channel) {
		return fmt.Errorf("notify: invalid channel")
	}
	if !utf8.ValidString(payload) {
		return fmt.Errorf("notify: payload is not utf-8")
	}
	if len(payload) > maxPayload {
		payload = payload[:maxPayload]
		for !utf8.ValidString(payload) {
			payload = payload[:len(payload)-1]
		}
	}
	return db.WithContext(ctx).Exec("SELECT pg_notify(?, ?)", channel, payload).Error
}

// Listen 占用一条池连接直到 ctx 取消。channel 必须是本包常量。
// pool 为 nil、LISTEN channel 非法或 Acquire 失败时返回 error；ctx 取消后退出循环。
func Listen(ctx context.Context, pool *pgxpool.Pool, channels ...string) (<-chan Notification, error) {
	if pool == nil {
		return nil, ErrNoPool
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("notify: no channels")
	}
	for _, channel := range channels {
		if !ValidChannel(channel) || !safeIdent(channel) {
			return nil, fmt.Errorf("notify: invalid channel")
		}
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			conn.Release()
			return nil, err
		}
	}
	out := make(chan Notification, 32)
	ListenerConnections.Add(1)
	go func() {
		defer ListenerConnections.Add(-1)
		defer close(out)
		defer conn.Release()
		for {
			n, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				return
			}
			note := Notification{Channel: n.Channel, Payload: n.Payload}
			select {
			case out <- note:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func safeIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
