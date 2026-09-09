// Package queue owns transactional River submission and worker integration.
package queue

import (
	"context"
	"errors"
)

var ErrBusy = errors.New("queue: aggregate already running")
var ErrLater = errors.New("queue: retry later")

// ActorFunc returns nil only after its business outcome is durable. Temporary
// infrastructure errors use River retries; capacity waits return ErrBusy/ErrLater.
type ActorFunc func(context.Context, string) error
