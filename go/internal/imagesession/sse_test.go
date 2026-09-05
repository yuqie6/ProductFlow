package imagesession

import (
	"bytes"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteSessionStatusOnlySuppressesIdenticalSnapshots(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	status := StatusResponse{
		ID: "session", HasActiveGenerationTask: true,
		GenerationTasks: []TaskResponse{{ID: "task", Status: "queued", ProgressMetadata: map[string]any{"a": 1, "b": 2}}},
	}
	previous, err := writeSessionStatus(c, status, nil)
	if err != nil {
		t.Fatal(err)
	}
	initialBytes := recorder.Body.Len()
	status.GenerationTasks[0].ProgressMetadata = map[string]any{"b": 2, "a": 1}
	previous, err = writeSessionStatus(c, status, previous)
	if err != nil || recorder.Body.Len() != initialBytes {
		t.Fatalf("equivalent snapshot was not suppressed: err=%v", err)
	}
	status.GenerationTasks[0].ProgressMetadata["a"] = 3
	previous, err = writeSessionStatus(c, status, previous)
	if err != nil || recorder.Body.Len() == initialBytes {
		t.Fatalf("task progress was suppressed: err=%v", err)
	}
	progressBytes := recorder.Body.Len()
	position := 2
	status.GenerationTasks[0].QueuePosition = &position
	previous, err = writeSessionStatus(c, status, previous)
	if err != nil || recorder.Body.Len() == progressBytes {
		t.Fatalf("queue-only change was suppressed: err=%v", err)
	}
	beforeInvalid := recorder.Body.Len()
	status.GenerationTasks[0].ProgressMetadata["invalid"] = math.NaN()
	unchanged, err := writeSessionStatus(c, status, previous)
	if err == nil || !bytes.Equal(unchanged, previous) || recorder.Body.Len() != beforeInvalid {
		t.Fatal("marshal failure advanced snapshot or wrote a partial frame")
	}
	status.HasActiveGenerationTask = false
	status.GenerationTasks = []TaskResponse{}
	if _, err := writeSessionStatus(c, status, previous); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(recorder.Body.String(), "event: session.status\n"); got != 4 {
		t.Fatalf("frames=%d, want initial/progress/queue/terminal", got)
	}
	// A fresh connection must receive even the same terminal snapshot.
	reconnected := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(reconnected)
	if _, err := writeSessionStatus(c, status, nil); err != nil || reconnected.Body.Len() == 0 {
		t.Fatalf("reconnect lost initial snapshot: err=%v", err)
	}
}
