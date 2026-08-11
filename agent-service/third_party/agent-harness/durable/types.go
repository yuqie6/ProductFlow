// Package durable turns tool calls into journaled jobs with explicit crash semantics.
package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const EventSchemaVersion = 1

type EffectClass string

const (
	EffectPure         EffectClass = "pure"
	EffectIdempotent   EffectClass = "idempotent"
	EffectReconcilable EffectClass = "reconcilable"
	EffectOpaque       EffectClass = "opaque"
)

func (e EffectClass) valid() bool {
	switch e {
	case EffectPure, EffectIdempotent, EffectReconcilable, EffectOpaque:
		return true
	default:
		return false
	}
}

type JobStatus string

const (
	JobPending        JobStatus = "pending"
	JobRunning        JobStatus = "running"
	JobAwaitingSteps  JobStatus = "awaiting_steps"
	JobSucceeded      JobStatus = "succeeded"
	JobFailed         JobStatus = "failed"
	JobUnknown        JobStatus = "unknown"
	JobRequiresAction JobStatus = "requires_action"
)

func (s JobStatus) valid() bool {
	switch s {
	case JobPending, JobRunning, JobAwaitingSteps, JobSucceeded, JobFailed, JobUnknown, JobRequiresAction:
		return true
	default:
		return false
	}
}

type JobKind string

const (
	JobKindToolSequence JobKind = "tool_sequence/v1"
	JobKindAgentTurn    JobKind = "agent_turn/v1"
)

func (k JobKind) valid() bool {
	switch k {
	case JobKindToolSequence, JobKindAgentTurn:
		return true
	default:
		return false
	}
}

type StepStatus string

const (
	StepPending        StepStatus = "pending"
	StepPrepared       StepStatus = "prepared"
	StepRunning        StepStatus = "running"
	StepSucceeded      StepStatus = "succeeded"
	StepFailed         StepStatus = "failed"
	StepUnknown        StepStatus = "unknown"
	StepRequiresAction StepStatus = "requires_action"
)

type AttemptStatus string

const (
	AttemptPrepared       AttemptStatus = "prepared"
	AttemptRunning        AttemptStatus = "running"
	AttemptSucceeded      AttemptStatus = "succeeded"
	AttemptFailed         AttemptStatus = "failed"
	AttemptUnknown        AttemptStatus = "unknown"
	AttemptRequiresAction AttemptStatus = "requires_action"
)

type StepSpec struct {
	ID                string          `json:"id,omitempty"`
	Tool              string          `json:"tool"`
	Input             json.RawMessage `json:"input"`
	AwaitContinuation bool            `json:"await_continuation,omitempty"`
}

type JobSpec struct {
	ID    string     `json:"id,omitempty"`
	Kind  JobKind    `json:"kind,omitempty"`
	Name  string     `json:"name"`
	Steps []StepSpec `json:"steps"`
}

type Job struct {
	ID             string     `json:"id"`
	Kind           JobKind    `json:"kind"`
	Name           string     `json:"name"`
	Status         JobStatus  `json:"status"`
	Error          string     `json:"error,omitempty"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Steps          []Step     `json:"steps"`
}

type ListOptions struct {
	Statuses    []JobStatus `json:"statuses,omitempty"`
	Kinds       []JobKind   `json:"kinds,omitempty"`
	Limit       int         `json:"limit,omitempty"`
	OldestFirst bool        `json:"oldest_first,omitempty"`
}

type Step struct {
	ID       string          `json:"id"`
	JobID    string          `json:"job_id"`
	Position int             `json:"position"`
	Tool     string          `json:"tool"`
	Effect   EffectClass     `json:"effect"`
	Input    json.RawMessage `json:"input"`
	Status   StepStatus      `json:"status"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	// AwaitContinuation keeps the job open after this becomes the final
	// successful step. An orchestrator must append more steps or complete it.
	AwaitContinuation bool      `json:"await_continuation,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Attempt struct {
	ID             string          `json:"id"`
	JobID          string          `json:"job_id"`
	StepID         string          `json:"step_id"`
	Number         int             `json:"number"`
	IdempotencyKey string          `json:"idempotency_key"`
	Status         AttemptStatus   `json:"status"`
	Prepared       json.RawMessage `json:"prepared,omitempty"`
	Checkpoint     json.RawMessage `json:"checkpoint,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	LeaseOwner     string          `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Event struct {
	Sequence      int64           `json:"sequence"`
	SchemaVersion int             `json:"schema_version"`
	JobID         string          `json:"job_id"`
	StepID        string          `json:"step_id,omitempty"`
	AttemptID     string          `json:"attempt_id,omitempty"`
	Kind          string          `json:"kind"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Invocation struct {
	JobID            string
	StepID           string
	AttemptID        string
	IdempotencyKey   string
	Input            json.RawMessage
	Prepared         json.RawMessage
	Checkpoint       json.RawMessage
	recordCheckpoint func(context.Context, json.RawMessage) error
}

// RecordCheckpoint durably records external state needed to reconcile an
// in-flight effect. The checkpoint must not contain credentials.
func (i Invocation) RecordCheckpoint(ctx context.Context, checkpoint json.RawMessage) error {
	if i.recordCheckpoint == nil {
		return errors.New("durable invocation 不支持执行中 checkpoint")
	}
	return i.recordCheckpoint(ctx, cloneJSON(checkpoint))
}

type ReconcileState string

const (
	ReconcileApplied    ReconcileState = "applied"
	ReconcileNotApplied ReconcileState = "not_applied"
	ReconcileConflict   ReconcileState = "conflict"
	ReconcileUnknown    ReconcileState = "unknown"
)

type ReconcileResult struct {
	State  ReconcileState
	Result json.RawMessage
	Detail string
}

type ResolutionOutcome string

const (
	ResolutionApplied ResolutionOutcome = "applied"
	ResolutionFailed  ResolutionOutcome = "failed"
	ResolutionRetry   ResolutionOutcome = "retry"
)

// UnknownResolution records an operator decision for one opaque effect whose
// outcome could not be inferred after worker loss.
type UnknownResolution struct {
	StepID    string            `json:"step_id"`
	AttemptID string            `json:"attempt_id"`
	Outcome   ResolutionOutcome `json:"outcome"`
	Actor     string            `json:"actor"`
	Reason    string            `json:"reason"`
	Result    json.RawMessage   `json:"result,omitempty"`
}

// RequiredActionResolution records an operator decision for a step paused on
// an explicit precondition conflict or human gate.
type RequiredActionResolution struct {
	StepID    string            `json:"step_id"`
	AttemptID string            `json:"attempt_id"`
	Outcome   ResolutionOutcome `json:"outcome"`
	Actor     string            `json:"actor"`
	Reason    string            `json:"reason"`
	Result    json.RawMessage   `json:"result,omitempty"`
}

// ResolutionTarget binds an operator decision to one exact journal attempt.
type ResolutionTarget struct {
	StepID    string `json:"step_id"`
	AttemptID string `json:"attempt_id"`
}

// Tool defines the recovery contract for one durable effect.
type Tool interface {
	Name() string
	Effect() EffectClass
	Prepare(context.Context, json.RawMessage) (json.RawMessage, error)
	Execute(context.Context, Invocation) (json.RawMessage, error)
	Reconcile(context.Context, Invocation) (ReconcileResult, error)
}

type FuncTool struct {
	ToolName      string
	EffectClass   EffectClass
	PrepareFunc   func(context.Context, json.RawMessage) (json.RawMessage, error)
	ExecuteFunc   func(context.Context, Invocation) (json.RawMessage, error)
	ReconcileFunc func(context.Context, Invocation) (ReconcileResult, error)
}

func (t FuncTool) Name() string        { return t.ToolName }
func (t FuncTool) Effect() EffectClass { return t.EffectClass }
func (t FuncTool) Prepare(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.PrepareFunc != nil {
		return t.PrepareFunc(ctx, cloneJSON(input))
	}
	return cloneJSON(input), nil
}
func (t FuncTool) Execute(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	if t.ExecuteFunc == nil {
		return nil, errors.New("durable tool 缺少 ExecuteFunc")
	}
	return t.ExecuteFunc(ctx, cloneInvocation(invocation))
}
func (t FuncTool) Reconcile(ctx context.Context, invocation Invocation) (ReconcileResult, error) {
	if t.ReconcileFunc == nil {
		return ReconcileResult{}, errors.New("durable tool 缺少 ReconcileFunc")
	}
	return t.ReconcileFunc(ctx, cloneInvocation(invocation))
}

type FaultPoint string

const (
	FaultAfterPrepared FaultPoint = "after_prepared"
	FaultBeforeEffect  FaultPoint = "before_effect"
	FaultAfterEffect   FaultPoint = "after_effect_before_commit"
	FaultAfterCommit   FaultPoint = "after_commit"
)

type FaultContext struct {
	JobID     string
	StepID    string
	AttemptID string
}

type Options struct {
	Owner             string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	FaultInjector     func(FaultPoint, FaultContext)
}

var (
	ErrNotFound  = errors.New("durable job 不存在")
	ErrLeaseHeld = errors.New("durable attempt 正由其他 worker 执行")
	ErrJobFailed = errors.New("durable job 执行失败")
	ErrUnknown   = errors.New("durable effect 结果未知")
	// ErrOutcomeUnknown is returned by a running tool when it knows an effect
	// may have happened but cannot determine the outcome. Unlike an ordinary
	// tool error, the engine persists UNKNOWN immediately and never retries it.
	ErrOutcomeUnknown = errors.New("durable effect outcome ambiguous")
	ErrRequiresAction = errors.New("durable job 需要人工处理")
	ErrConflict       = errors.New("durable effect 前置条件冲突")
	ErrInvalidState   = errors.New("durable job 当前状态不允许该操作")
)

func terminalError(status JobStatus) error {
	switch status {
	case JobFailed:
		return ErrJobFailed
	case JobUnknown:
		return ErrUnknown
	case JobRequiresAction:
		return ErrRequiresAction
	default:
		return nil
	}
}

func validateTool(tool Tool) error {
	if tool == nil || tool.Name() == "" {
		return errors.New("durable tool 缺少名称")
	}
	if !tool.Effect().valid() {
		return fmt.Errorf("durable tool %s 的副作用类别无效: %q", tool.Name(), tool.Effect())
	}
	return nil
}

func normalizeJSON(value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(value) {
		return nil, errors.New("不是有效 JSON")
	}
	return cloneJSON(value), nil
}

func cloneJSON(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneInvocation(value Invocation) Invocation {
	value.Input = cloneJSON(value.Input)
	value.Prepared = cloneJSON(value.Prepared)
	value.Checkpoint = cloneJSON(value.Checkpoint)
	return value
}
