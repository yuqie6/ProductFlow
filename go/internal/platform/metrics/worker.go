package metrics

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// consumeResultNames is the closed set of worker Consume outcomes. Do not add job ids or actor names.
var consumeResultNames = []string{"consumed", "busy", "later", "failed", "unknown"}

var consumeResults = make(map[string]*atomic.Int64, len(consumeResultNames))
var consumeDurations = &recoveryHistogram{}

func init() {
	for _, name := range consumeResultNames {
		consumeResults[name] = &atomic.Int64{}
	}
}

// ObserveConsumeResult increments a bounded Consume outcome counter. Unknown names are ignored.
func ObserveConsumeResult(result string) {
	if counter, ok := consumeResults[result]; ok {
		counter.Add(1)
	}
}

// ConsumeResultCount returns the process-local Consume outcome counter.
func ConsumeResultCount(result string) int64 {
	if counter, ok := consumeResults[result]; ok {
		return counter.Load()
	}
	return 0
}

// ObserveConsumeDuration records one Consume handler run. Consume handles a single dispatch (no batch size).
func ObserveConsumeDuration(elapsed time.Duration) {
	consumeDurations.observe(elapsed)
}

func writeWorkerProcessSeries(b *strings.Builder) {
	writeUnlabeledHistogram(b, "productflow_advisory_lock_wait_seconds", "Wait time for pg_advisory_xact_lock on the generation capacity key.", advisoryLockWait)
	writeGenerationAdmissionDenied(b)
	b.WriteString("# HELP productflow_consume_results_total Worker Consume outcomes by bounded result.\n")
	b.WriteString("# TYPE productflow_consume_results_total counter\n")
	for _, name := range consumeResultNames {
		fmt.Fprintf(b, "productflow_consume_results_total{result=%q} %d\n", name, consumeResults[name].Load())
	}
	writeUnlabeledHistogram(b, "productflow_consume_duration_seconds", "Worker Consume handler duration.", consumeDurations)
}
