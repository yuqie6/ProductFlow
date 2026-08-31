package agent

import (
	"encoding/json"
	"testing"
)

func TestProjectTurnEventUnknownKindsRespectIgnorableContract(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"value":"preserved only for diagnostics"}`)

	kind, projected := projectTurnEvent(eventRow{Kind: "future/optional", Ignorable: true, Payload: payload})
	if kind != "agent.ignored" || projected["raw_kind"] != "future/optional" {
		t.Fatalf("ignorable event projected as %q %#v", kind, projected)
	}

	kind, projected = projectTurnEvent(eventRow{Kind: "future/required", Payload: payload})
	if kind != "agent.invalid" || projected["raw_kind"] != "future/required" {
		t.Fatalf("required event projected as %q %#v", kind, projected)
	}
}
