// Package notify 用 PostgreSQL LISTEN/NOTIFY 做跨进程唤醒。
// 写路径走 GORM `pg_notify`（随事务提交投递）；读路径需要独立 pgx 连接，因为 LISTEN 绑在会话上。
package notify

import (
	"context"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

const (
	ChannelControl      = "productflow_control"
	ChannelRun          = "productflow_run"
	ChannelTurn         = "productflow_turn"
	ChannelImageSession = "productflow_image_session"
	maxPayload          = 7900
)

var ErrNoPool = errors.New("notify: pgx pool is nil")

type Notification struct {
	Channel string
	Payload string
}

func ValidChannel(name string) bool {
	switch name {
	case ChannelControl, ChannelRun, ChannelTurn, ChannelImageSession:
		return true
	default:
		return false
	}
}

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
	go func() {
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
