package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type Engine struct {
	store             *sqliteStore
	tools             map[string]Tool
	owner             string
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	faultInjector     func(FaultPoint, FaultContext)
}

func Open(path string, options Options, registered ...Tool) (*Engine, error) {
	registry := make(map[string]Tool, len(registered))
	for _, tool := range registered {
		if err := validateTool(tool); err != nil {
			return nil, err
		}
		if registry[tool.Name()] != nil {
			return nil, fmt.Errorf("durable tool 重复注册: %s", tool.Name())
		}
		registry[tool.Name()] = tool
	}
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		random, err := newID("worker")
		if err != nil {
			return nil, err
		}
		owner = fmt.Sprintf("%s-pid%d", random, os.Getpid())
	}
	leaseTTL := options.LeaseTTL
	if leaseTTL == 0 {
		leaseTTL = 15 * time.Second
	}
	if leaseTTL < 100*time.Millisecond {
		return nil, errors.New("LeaseTTL 不能小于 100ms")
	}
	heartbeat := options.HeartbeatInterval
	if heartbeat == 0 {
		heartbeat = leaseTTL / 3
	}
	if heartbeat <= 0 || heartbeat >= leaseTTL {
		return nil, errors.New("HeartbeatInterval 必须大于 0 且小于 LeaseTTL")
	}
	store, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &Engine{
		store: store, tools: registry, owner: owner, leaseTTL: leaseTTL,
		heartbeatInterval: heartbeat, faultInjector: options.FaultInjector,
	}, nil
}

func (e *Engine) Close() error {
	if e == nil || e.store == nil {
		return nil
	}
	return e.store.close()
}

func (e *Engine) Submit(ctx context.Context, spec JobSpec) (Job, error) {
	return e.store.submit(ctx, spec, e.tools, time.Now().UTC())
}

func (e *Engine) Get(ctx context.Context, jobID string) (Job, error) {
	return e.store.getJob(ctx, jobID)
}

func (e *Engine) Attempts(ctx context.Context, jobID string) ([]Attempt, error) {
	return e.store.attempts(ctx, jobID)
}

func (e *Engine) Events(ctx context.Context, jobID string, afterSequence int64) ([]Event, error) {
	return e.store.events(ctx, jobID, afterSequence)
}

func (e *Engine) List(ctx context.Context, options ListOptions) ([]Job, error) {
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 500 {
		return nil, errors.New("durable list limit 必须在 1 到 500 之间")
	}
	seen := make(map[JobStatus]bool, len(options.Statuses))
	statuses := make([]JobStatus, 0, len(options.Statuses))
	for _, status := range options.Statuses {
		if !status.valid() {
			return nil, fmt.Errorf("durable list status 无效: %q", status)
		}
		if !seen[status] {
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	options.Statuses = statuses
	seenKinds := make(map[JobKind]bool, len(options.Kinds))
	kinds := make([]JobKind, 0, len(options.Kinds))
	for _, kind := range options.Kinds {
		if !kind.valid() {
			return nil, fmt.Errorf("durable list kind 无效: %q", kind)
		}
		if !seenKinds[kind] {
			seenKinds[kind] = true
			kinds = append(kinds, kind)
		}
	}
	options.Kinds = kinds
	return e.store.listJobs(ctx, options)
}

// ResolveUnknown applies an audited operator decision to the current opaque
// unknown step. A retry only prepares a new attempt; it never executes it.
func (e *Engine) ResolveUnknown(ctx context.Context, jobID string, resolution UnknownResolution) (Job, error) {
	resolution.StepID = strings.TrimSpace(resolution.StepID)
	resolution.AttemptID = strings.TrimSpace(resolution.AttemptID)
	resolution.Actor = strings.TrimSpace(resolution.Actor)
	resolution.Reason = strings.TrimSpace(resolution.Reason)
	if resolution.StepID == "" || len(resolution.StepID) > 300 {
		return Job{}, errors.New("unknown resolution step_id 不能为空且不能超过 300 字符")
	}
	if resolution.AttemptID == "" || len(resolution.AttemptID) > 300 {
		return Job{}, errors.New("unknown resolution attempt_id 不能为空且不能超过 300 字符")
	}
	if resolution.Actor == "" || len(resolution.Actor) > 200 {
		return Job{}, errors.New("unknown resolution actor 不能为空且不能超过 200 字符")
	}
	if resolution.Reason == "" || len(resolution.Reason) > 2000 {
		return Job{}, errors.New("unknown resolution reason 不能为空且不能超过 2000 字符")
	}
	switch resolution.Outcome {
	case ResolutionApplied:
		result, err := normalizeJSON(resolution.Result)
		if err != nil {
			return Job{}, fmt.Errorf("unknown resolution result: %w", err)
		}
		resolution.Result = result
	case ResolutionFailed, ResolutionRetry:
		if len(resolution.Result) != 0 {
			return Job{}, errors.New("failed/retry resolution 不能携带 result")
		}
	default:
		return Job{}, fmt.Errorf("unknown resolution outcome 无效: %q", resolution.Outcome)
	}
	if err := e.store.resolveUnknown(ctx, strings.TrimSpace(jobID), resolution, time.Now().UTC()); err != nil {
		return Job{}, err
	}
	return e.store.getJob(ctx, strings.TrimSpace(jobID))
}

// RunNext claims and runs the oldest runnable job supported by this Engine.
func (e *Engine) RunNext(ctx context.Context) (Job, bool, error) {
	supported := make([]string, 0, len(e.tools))
	for name := range e.tools {
		supported = append(supported, name)
	}
	sort.Strings(supported)
	ids, err := e.store.runnableJobIDs(ctx, supported, 64, time.Now().UTC())
	if err != nil {
		return Job{}, false, err
	}
	for _, id := range ids {
		job, err := e.store.getJob(ctx, id)
		if err != nil {
			return Job{}, false, err
		}
		if err := e.validateJobTools(job); err != nil {
			return Job{}, false, err
		}
		if _, err = e.store.claimJob(ctx, id, e.owner, e.leaseTTL, time.Now().UTC()); err != nil {
			if errors.Is(err, ErrLeaseHeld) || errors.Is(err, ErrInvalidState) {
				continue
			}
			return Job{}, false, err
		}
		job, err = e.runWithJobHeartbeat(ctx, id)
		if errors.Is(err, ErrLeaseHeld) {
			continue
		}
		return job, true, err
	}
	return Job{}, false, nil
}

// Run executes or recovers a job until it reaches a terminal state.
func (e *Engine) Run(ctx context.Context, jobID string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return e.jobWithError(context.Background(), jobID, err)
	}
	job, err := e.store.getJob(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if terminal := terminalError(job.Status); terminal != nil {
		return job, wrapJobError(terminal, job.Error)
	}
	if job.Status == JobSucceeded || job.Status == JobAwaitingSteps {
		return job, nil
	}
	if err := e.validateJobTools(job); err != nil {
		return job, err
	}
	if _, err := e.store.claimJob(ctx, job.ID, e.owner, e.leaseTTL, time.Now().UTC()); err != nil {
		if errors.Is(err, ErrInvalidState) {
			latest, getErr := e.store.getJob(ctx, job.ID)
			if getErr == nil {
				if latest.Status == JobSucceeded || latest.Status == JobAwaitingSteps {
					return latest, nil
				}
				if terminal := terminalError(latest.Status); terminal != nil {
					return latest, wrapJobError(terminal, latest.Error)
				}
			}
		}
		return job, err
	}
	return e.runWithJobHeartbeat(ctx, job.ID)
}

func (e *Engine) runWithJobHeartbeat(ctx context.Context, jobID string) (Job, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatDone := make(chan error, 1)
	go func() {
		err := e.jobHeartbeat(runCtx, jobID)
		if err != nil {
			cancel()
		}
		heartbeatDone <- err
	}()
	job, runErr := e.runClaimed(runCtx, jobID)
	cancel()
	heartbeatErr := <-heartbeatDone
	if runErr != nil || heartbeatErr != nil {
		latest, err := e.store.getJob(context.Background(), jobID)
		if err != nil {
			return Job{}, errors.Join(runErr, heartbeatErr, err)
		}
		job = latest
	}
	if job.Status == JobSucceeded || job.Status == JobAwaitingSteps {
		return job, nil
	}
	if terminal := terminalError(job.Status); terminal != nil {
		return job, wrapJobError(terminal, job.Error)
	}
	if runErr != nil && heartbeatErr != nil {
		return job, errors.Join(runErr, heartbeatErr)
	}
	if heartbeatErr != nil {
		return job, fmt.Errorf("job heartbeat: %w", heartbeatErr)
	}
	return job, runErr
}

func (e *Engine) runClaimed(ctx context.Context, jobID string) (Job, error) {
	for {
		if err := ctx.Err(); err != nil {
			return e.jobWithError(context.Background(), jobID, err)
		}
		job, err := e.store.getJob(ctx, jobID)
		if err != nil {
			return Job{}, err
		}
		if terminal := terminalError(job.Status); terminal != nil {
			return job, wrapJobError(terminal, job.Error)
		}
		if job.Status == JobSucceeded || job.Status == JobAwaitingSteps {
			return job, nil
		}
		step, ok := nextIncompleteStep(job)
		if !ok {
			return job, errors.New("durable job 状态不一致: 没有待执行步骤但尚未成功")
		}
		tool := e.tools[step.Tool]
		if tool == nil {
			return job, fmt.Errorf("恢复需要未注册工具 %q", step.Tool)
		}
		if tool.Effect() != step.Effect {
			return job, fmt.Errorf("工具 %s 的副作用契约从 %s 变为 %s,拒绝恢复", step.Tool, step.Effect, tool.Effect())
		}

		attempt, exists, err := e.store.latestAttempt(ctx, step.ID)
		if err != nil {
			return job, err
		}
		if !exists {
			prepared, prepareErr := tool.Prepare(ctx, cloneJSON(step.Input))
			if prepareErr != nil {
				status := AttemptFailed
				terminal := ErrJobFailed
				if errors.Is(prepareErr, ErrConflict) {
					status = AttemptRequiresAction
					terminal = ErrRequiresAction
				}
				if err := e.store.recordPreparationFailure(ctx, job.ID, step.ID, status, prepareErr.Error(), time.Now().UTC()); err != nil {
					return job, err
				}
				return e.jobWithError(ctx, job.ID, terminal)
			}
			attempt, err = e.store.createPreparedAttempt(ctx, job.ID, step.ID, prepared, time.Now().UTC())
			if err != nil {
				return job, err
			}
			e.fault(FaultAfterPrepared, attempt)
		}

		switch attempt.Status {
		case AttemptPrepared:
			if err := e.claimAndExecute(ctx, tool, step, attempt); err != nil {
				return e.jobWithError(context.Background(), job.ID, err)
			}
		case AttemptRunning:
			if attempt.LeaseExpiresAt != nil && attempt.LeaseExpiresAt.After(time.Now().UTC()) {
				return job, ErrLeaseHeld
			}
			if err := e.recoverRunning(ctx, tool, step, attempt); err != nil {
				return e.jobWithError(context.Background(), job.ID, err)
			}
		case AttemptSucceeded:
			return job, errors.New("durable journal 不一致: attempt 已成功但 step 未成功")
		case AttemptFailed:
			return job, wrapJobError(ErrJobFailed, attempt.Error)
		case AttemptUnknown:
			return job, wrapJobError(ErrUnknown, attempt.Error)
		case AttemptRequiresAction:
			return job, wrapJobError(ErrRequiresAction, attempt.Error)
		default:
			return job, fmt.Errorf("未知 attempt 状态 %q", attempt.Status)
		}
	}
}

func (e *Engine) validateJobTools(job Job) error {
	for _, step := range job.Steps {
		if step.Status == StepSucceeded {
			continue
		}
		tool := e.tools[step.Tool]
		if tool == nil {
			return fmt.Errorf("恢复需要未注册工具 %q", step.Tool)
		}
		if tool.Effect() != step.Effect {
			return fmt.Errorf("工具 %s 的副作用契约从 %s 变为 %s,拒绝恢复", step.Tool, step.Effect, tool.Effect())
		}
	}
	return nil
}

func (e *Engine) claimAndExecute(ctx context.Context, tool Tool, step Step, attempt Attempt) error {
	claimed, err := e.store.claimAttempt(ctx, attempt, e.owner, e.leaseTTL, time.Now().UTC())
	if err != nil {
		return err
	}
	e.fault(FaultBeforeEffect, claimed)
	return e.executeClaimed(ctx, tool, step, claimed)
}

func (e *Engine) recoverRunning(ctx context.Context, tool Tool, step Step, attempt Attempt) error {
	if tool.Effect() == EffectOpaque {
		if err := e.store.markOpaqueUnknown(ctx, attempt, time.Now().UTC()); err != nil {
			return err
		}
		return ErrUnknown
	}
	claimed, err := e.store.claimAttempt(ctx, attempt, e.owner, e.leaseTTL, time.Now().UTC())
	if err != nil {
		return err
	}
	if tool.Effect() != EffectReconcilable {
		e.fault(FaultBeforeEffect, claimed)
		return e.executeClaimed(ctx, tool, step, claimed)
	}

	invocation := e.invocationFor(step, claimed)
	reconciled, reconcileErr := tool.Reconcile(ctx, invocation)
	if reconcileErr != nil {
		message := "恢复对账失败: " + reconcileErr.Error()
		if err := e.store.finishAttemptWithError(ctx, claimed, e.owner, AttemptRequiresAction, message, nil, time.Now().UTC()); err != nil {
			return err
		}
		return ErrRequiresAction
	}
	switch reconciled.State {
	case ReconcileApplied:
		if err := e.store.completeAttempt(ctx, claimed, e.owner, reconciled.Result, time.Now().UTC()); err != nil {
			return err
		}
		e.fault(FaultAfterCommit, claimed)
		return nil
	case ReconcileNotApplied:
		e.fault(FaultBeforeEffect, claimed)
		return e.executeClaimed(ctx, tool, step, claimed)
	case ReconcileConflict:
		message := strings.TrimSpace(reconciled.Detail)
		if message == "" {
			message = "恢复对账发现状态冲突"
		}
		if err := e.store.finishAttemptWithError(ctx, claimed, e.owner, AttemptRequiresAction, message, nil, time.Now().UTC()); err != nil {
			return err
		}
		return ErrRequiresAction
	case ReconcileUnknown:
		message := strings.TrimSpace(reconciled.Detail)
		if message == "" {
			message = "恢复对账仍无法判定外部副作用结果"
		}
		if err := e.store.finishAttemptWithError(ctx, claimed, e.owner, AttemptUnknown, message, nil, time.Now().UTC()); err != nil {
			return err
		}
		return ErrUnknown
	default:
		message := fmt.Sprintf("工具返回无效 reconcile 状态 %q", reconciled.State)
		if err := e.store.finishAttemptWithError(ctx, claimed, e.owner, AttemptRequiresAction, message, nil, time.Now().UTC()); err != nil {
			return err
		}
		return ErrRequiresAction
	}
}

func (e *Engine) executeClaimed(ctx context.Context, tool Tool, step Step, attempt Attempt) error {
	runCtx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		err := e.heartbeat(runCtx, attempt.ID)
		if err != nil {
			cancel()
		}
		heartbeatDone <- err
	}()
	result, toolErr := tool.Execute(runCtx, e.invocationFor(step, attempt))
	cancel()
	heartbeatErr := <-heartbeatDone
	e.fault(FaultAfterEffect, attempt)
	if heartbeatErr != nil {
		return fmt.Errorf("attempt heartbeat: %w", heartbeatErr)
	}
	if err := ctx.Err(); err != nil {
		// 保持 running;lease 过期后按副作用契约恢复。
		return err
	}
	if toolErr != nil {
		status := AttemptFailed
		terminal := ErrJobFailed
		if errors.Is(toolErr, ErrOutcomeUnknown) {
			status = AttemptUnknown
			terminal = ErrUnknown
		} else if errors.Is(toolErr, ErrConflict) {
			status = AttemptRequiresAction
			terminal = ErrRequiresAction
		}
		var failureResult json.RawMessage
		if len(result) > 0 && json.Valid(result) {
			failureResult = result
		}
		if err := e.store.finishAttemptWithError(context.Background(), attempt, e.owner, status, toolErr.Error(), failureResult, time.Now().UTC()); err != nil {
			return err
		}
		return terminal
	}
	result, err := normalizeJSON(result)
	if err != nil {
		// 结果协议损坏时不提交终态;恢复路径会对账或将 opaque 标为 UNKNOWN。
		return fmt.Errorf("工具 %s 返回无效 JSON: %w", tool.Name(), err)
	}
	if err := e.store.completeAttempt(context.Background(), attempt, e.owner, result, time.Now().UTC()); err != nil {
		return err
	}
	e.fault(FaultAfterCommit, attempt)
	return nil
}

func (e *Engine) heartbeat(ctx context.Context, attemptID string) error {
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.store.heartbeat(ctx, attemptID, e.owner, e.leaseTTL, time.Now().UTC()); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (e *Engine) jobHeartbeat(ctx context.Context, jobID string) error {
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.store.heartbeatJob(ctx, jobID, e.owner, e.leaseTTL, time.Now().UTC()); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (e *Engine) fault(point FaultPoint, attempt Attempt) {
	if e.faultInjector != nil {
		e.faultInjector(point, FaultContext{JobID: attempt.JobID, StepID: attempt.StepID, AttemptID: attempt.ID})
	}
}

func (e *Engine) jobWithError(ctx context.Context, jobID string, cause error) (Job, error) {
	job, err := e.store.getJob(ctx, jobID)
	if err != nil {
		return Job{}, errors.Join(cause, err)
	}
	if terminal := terminalError(job.Status); terminal != nil {
		return job, wrapJobError(terminal, job.Error)
	}
	if job.Status == JobSucceeded || job.Status == JobAwaitingSteps {
		return job, nil
	}
	return job, cause
}

func nextIncompleteStep(job Job) (Step, bool) {
	for _, step := range job.Steps {
		if step.Status != StepSucceeded {
			return step, true
		}
	}
	return Step{}, false
}

func (e *Engine) invocationFor(step Step, attempt Attempt) Invocation {
	invocation := Invocation{
		JobID: attempt.JobID, StepID: step.ID, AttemptID: attempt.ID,
		IdempotencyKey: attempt.IdempotencyKey, Input: cloneJSON(step.Input), Prepared: cloneJSON(attempt.Prepared),
		Checkpoint: cloneJSON(attempt.Checkpoint),
	}
	invocation.recordCheckpoint = func(ctx context.Context, checkpoint json.RawMessage) error {
		return e.store.saveAttemptCheckpoint(ctx, attempt, e.owner, checkpoint, time.Now().UTC())
	}
	return invocation
}

func wrapJobError(base error, detail string) error {
	if strings.TrimSpace(detail) == "" {
		return base
	}
	return fmt.Errorf("%w: %s", base, detail)
}
