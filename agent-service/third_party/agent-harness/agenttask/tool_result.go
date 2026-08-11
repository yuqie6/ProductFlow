package agenttask

import turnprotocol "github.com/yuqie6/agent-harness/turn"

type ToolResult = turnprotocol.ToolResult
type ToolResultContent = turnprotocol.ToolResultContent

const (
	ToolResultSchemaVersion      = turnprotocol.ToolResultSchemaVersion
	MaxImagesPerToolResult       = turnprotocol.MaxImagesPerToolResult
	MaxToolResultImageBytes      = turnprotocol.MaxToolResultImageBytes
	MaxTotalToolResultImageBytes = turnprotocol.MaxTotalToolResultImageBytes
)

func TextToolResult(text string) ToolResult { return turnprotocol.TextToolResult(text) }
