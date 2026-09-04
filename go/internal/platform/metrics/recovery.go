package metrics

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const recoveryHistogramBucketCount = 11

var recoveryDurationBuckets = [...]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

type recoveryHistogram struct {
	buckets  [recoveryHistogramBucketCount]atomic.Int64
	count    atomic.Int64
	sumNanos atomic.Int64
}

var recoveryDurations = make(map[string]*recoveryHistogram, len(recoveryBacklogDomains))
var recoveryLockDurations = make(map[string]*recoveryHistogram, len(recoveryBacklogDomains))
var recoveryErrors = make(map[string]*atomic.Int64, len(recoveryBacklogDomains))
var advisoryLockWait = &recoveryHistogram{}

func init() {
	for _, domain := range recoveryBacklogDomains {
		recoveryDurations[domain] = &recoveryHistogram{}
		recoveryLockDurations[domain] = &recoveryHistogram{}
		recoveryErrors[domain] = &atomic.Int64{}
	}
}

// ObserveRecovery records one dispatcher recovery domain run. Labels are restricted to the fixed domain set.
func ObserveRecovery(domain string, elapsed time.Duration, failed bool) {
	hist, ok := recoveryDurations[domain]
	if !ok {
		return
	}
	hist.observe(elapsed)
	if failed {
		recoveryErrors[domain].Add(1)
	}
}

// ObserveRecoveryLock records the candidate scan that acquires recovery row locks.
func ObserveRecoveryLock(domain string, elapsed time.Duration) {
	if hist, ok := recoveryLockDurations[domain]; ok {
		hist.observe(elapsed)
	}
}

// ObserveAdvisoryLockWait records wait time for pg_advisory_xact_lock on the generation capacity key.
func ObserveAdvisoryLockWait(elapsed time.Duration) {
	advisoryLockWait.observe(elapsed)
}

// AdvisoryLockWaitCount is the number of observed advisory lock waits in this process.
func AdvisoryLockWaitCount() int64 {
	return advisoryLockWait.count.Load()
}

func (hist *recoveryHistogram) observe(elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := elapsed.Seconds()
	for i, bucket := range recoveryDurationBuckets {
		if seconds <= bucket {
			hist.buckets[i].Add(1)
		}
	}
	hist.count.Add(1)
	hist.sumNanos.Add(elapsed.Nanoseconds())
}

func writeRecoveryHistograms(b *strings.Builder) {
	writeRecoveryHistogramSet(b, "productflow_recovery_duration_seconds", "Recovery duration by domain.", recoveryDurations)
	writeRecoveryHistogramSet(b, "productflow_recovery_lock_acquire_duration_seconds", "Recovery candidate lock acquisition query duration by domain.", recoveryLockDurations)
	b.WriteString("# HELP productflow_recovery_errors_total Recovery domain failures.\n")
	b.WriteString("# TYPE productflow_recovery_errors_total counter\n")
	for _, domain := range recoveryBacklogDomains {
		fmt.Fprintf(b, "productflow_recovery_errors_total{domain=%q} %d\n", domain, recoveryErrors[domain].Load())
	}
}

func writeUnlabeledHistogram(b *strings.Builder, name, help string, hist *recoveryHistogram) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
	for i, bucket := range recoveryDurationBuckets {
		fmt.Fprintf(b, "%s_bucket{le=%q} %d\n", name, formatMetricFloat(bucket), hist.buckets[i].Load())
	}
	count := hist.count.Load()
	fmt.Fprintf(b, "%s_bucket{le=\"+Inf\"} %d\n", name, count)
	fmt.Fprintf(b, "%s_sum %s\n", name, formatMetricFloat(float64(hist.sumNanos.Load())/float64(time.Second)))
	fmt.Fprintf(b, "%s_count %d\n", name, count)
}

func writeRecoveryHistogramSet(b *strings.Builder, name, help string, values map[string]*recoveryHistogram) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
	for _, domain := range recoveryBacklogDomains {
		hist := values[domain]
		for i, bucket := range recoveryDurationBuckets {
			fmt.Fprintf(b, "%s_bucket{domain=%q,le=%q} %d\n", name, domain, formatMetricFloat(bucket), hist.buckets[i].Load())
		}
		count := hist.count.Load()
		fmt.Fprintf(b, "%s_bucket{domain=%q,le=\"+Inf\"} %d\n", name, domain, count)
		fmt.Fprintf(b, "%s_sum{domain=%q} %s\n", name, domain, formatMetricFloat(float64(hist.sumNanos.Load())/float64(time.Second)))
		fmt.Fprintf(b, "%s_count{domain=%q} %d\n", name, domain, count)
	}
}

func formatMetricFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
