package agenttask

import (
	"context"

	"github.com/yuqie6/agent-harness/durable"
	"github.com/yuqie6/agent-harness/internal/llm"
	"github.com/yuqie6/agent-harness/internal/turninput"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

type TurnInput = turnprotocol.TurnInput
type InputContent = turnprotocol.InputContent
type InputImage = turnprotocol.InputImage
type ContentType = turnprotocol.ContentType
type ImageDetail = turnprotocol.ImageDetail
type ImageCheckpointMode = turnprotocol.ImageCheckpointMode

const (
	APIVersion               = turnprotocol.APIVersion
	TurnInputSchemaVersion   = turnprotocol.InputSchemaVersion
	ContentInputText         = turnprotocol.ContentInputText
	ContentInputImage        = turnprotocol.ContentInputImage
	ImageDetailAuto          = turnprotocol.ImageDetailAuto
	ImageDetailLow           = turnprotocol.ImageDetailLow
	ImageDetailHigh          = turnprotocol.ImageDetailHigh
	ImageCheckpointReference = turnprotocol.ImageCheckpointReference
	ImageCheckpointEmbed     = turnprotocol.ImageCheckpointEmbed
	MaxImagesPerTurn         = turnprotocol.MaxImagesPerTurn
	MaxImageBytes            = turnprotocol.MaxImageBytes
	MaxTotalImageBytes       = turnprotocol.MaxTotalImageBytes
)

func TextInput(text string) TurnInput { return turnprotocol.TextInput(text) }

func (r *Runner) SubmitTurn(ctx context.Context, input TurnInput) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	message, err := turninput.Message(input)
	if err != nil {
		return durable.Job{}, err
	}
	job, err := r.inner.SubmitMessage(ctx, turninput.Label(input), message)
	return job, publicError(err)
}

func (r *Runner) SubmitTurnWithID(ctx context.Context, turnID string, input TurnInput) (durable.Job, error) {
	if err := r.validate(); err != nil {
		return durable.Job{}, err
	}
	message, err := turninput.Message(input)
	if err != nil {
		return durable.Job{}, err
	}
	job, err := r.inner.SubmitMessageWithID(ctx, turnID, turninput.Label(input), message)
	return job, publicError(err)
}

func (r *Runner) submitTurnWithHistory(
	ctx context.Context,
	turnID string,
	input TurnInput,
	history []llm.Message,
) (durable.Job, error) {
	message, err := turninput.Message(input)
	if err != nil {
		return durable.Job{}, err
	}
	messages := append(llm.CloneMessages(history), message)
	job, err := r.inner.SubmitMessagesWithID(ctx, turnID, turninput.Label(input), messages)
	return job, publicError(err)
}

func (r *Runner) transcript(ctx context.Context, turnID string) ([]llm.Message, error) {
	messages, err := r.inner.Transcript(ctx, turnID)
	return messages, publicError(err)
}
