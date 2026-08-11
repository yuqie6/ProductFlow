package durableagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
)

var errStoredWaitTimeout = errors.New("stored response wait timeout")

func durableModelError(err error) error {
	if llm.IsAmbiguousRequest(err) || errors.Is(err, errStoredWaitTimeout) {
		return fmt.Errorf("%v: %w", err, durable.ErrOutcomeUnknown)
	}
	return err
}

type storedResponseCheckpoint struct {
	ResponseID string `json:"response_id"`
}

func createStoredChat(
	ctx context.Context,
	client StoredChatClient,
	invocation durable.Invocation,
	messages []llm.Message,
	tools []llm.Tool,
	waitTimeout time.Duration,
) (llm.Message, string, error) {
	handle, err := client.CreateStored(ctx, messages, tools, invocation.AttemptID)
	if err != nil {
		if llm.IsAmbiguousRequest(err) {
			return llm.Message{}, "", fmt.Errorf("stored response 创建结果未知: %w", durable.ErrOutcomeUnknown)
		}
		return llm.Message{}, "", err
	}
	checkpoint, err := json.Marshal(storedResponseCheckpoint{ResponseID: handle.ID})
	if err != nil {
		return llm.Message{}, "", err
	}
	if err := invocation.RecordCheckpoint(ctx, checkpoint); err != nil {
		return llm.Message{}, "", fmt.Errorf("远程 response %s 未能写入 journal: %v: %w", handle.ID, err, durable.ErrOutcomeUnknown)
	}
	return waitForStoredChat(ctx, client, handle.ID, waitTimeout)
}

func reconcileStoredChat(
	ctx context.Context,
	client StoredChatClient,
	invocation durable.Invocation,
	waitTimeout time.Duration,
) (llm.Message, string, durable.ReconcileState, error) {
	if len(invocation.Checkpoint) == 0 {
		return llm.Message{}, "", durable.ReconcileUnknown, nil
	}
	var checkpoint storedResponseCheckpoint
	if err := json.Unmarshal(invocation.Checkpoint, &checkpoint); err != nil || checkpoint.ResponseID == "" {
		return llm.Message{}, "", durable.ReconcileUnknown, nil
	}
	message, status, err := waitForStoredChat(ctx, client, checkpoint.ResponseID, waitTimeout)
	if err != nil {
		if errors.Is(err, errStoredWaitTimeout) {
			return llm.Message{}, status, durable.ReconcileUnknown, nil
		}
		if llm.IsStoredTerminal(err) {
			return llm.Message{}, status, durable.ReconcileConflict, err
		}
		return llm.Message{}, status, durable.ReconcileConflict, err
	}
	return message, status, durable.ReconcileApplied, nil
}

func waitForStoredChat(
	ctx context.Context,
	client StoredChatClient,
	responseID string,
	waitTimeout time.Duration,
) (llm.Message, string, error) {
	if waitTimeout <= 0 {
		return llm.Message{}, "", errors.New("stored response wait timeout 必须为正数")
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	delay := 100 * time.Millisecond
	lastStatus := ""
	var lastErr error
	for {
		result, err := client.RetrieveStored(waitCtx, responseID)
		lastStatus = result.Status
		lastErr = err
		if err == nil && result.Complete {
			return result.Message, result.Status, nil
		}
		if err != nil && llm.IsStoredTerminal(err) {
			return llm.Message{}, result.Status, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return llm.Message{}, lastStatus, ctxErr
			}
			detail := fmt.Sprintf("stored response %s 在 %s 内未达到终态", responseID, waitTimeout)
			if lastErr != nil {
				detail += ": " + lastErr.Error()
			} else if lastStatus != "" {
				detail += "; 最后状态 " + lastStatus
			}
			return llm.Message{}, lastStatus, fmt.Errorf("%w: %s", errStoredWaitTimeout, detail)
		case <-timer.C:
		}
		if delay < time.Second {
			delay *= 2
			if delay > time.Second {
				delay = time.Second
			}
		}
	}
}

func storedCheckpointDetail(invocation durable.Invocation) string {
	if len(invocation.Checkpoint) == 0 {
		return "stored response 在远程 ID 写入 journal 前失联"
	}
	var checkpoint storedResponseCheckpoint
	if err := json.Unmarshal(invocation.Checkpoint, &checkpoint); err != nil {
		return "stored response checkpoint 已损坏"
	}
	if checkpoint.ResponseID == "" {
		return "stored response checkpoint 缺少 response ID"
	}
	return ""
}
