package metrics

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// generationAdmissionDomains is the closed set of callers that share the
// PostgreSQL generation capacity lock. Do not add business IDs.
var generationAdmissionDomains = []string{"graph", "imagesession"}

var generationAdmissionDenied = make(map[string]*atomic.Int64, len(generationAdmissionDomains))

func init() {
	for _, domain := range generationAdmissionDomains {
		generationAdmissionDenied[domain] = &atomic.Int64{}
	}
}

// ObserveGenerationAdmissionDenied increments the capacity-full counter.
// Unknown domains are ignored.
func ObserveGenerationAdmissionDenied(domain string) {
	if counter, ok := generationAdmissionDenied[domain]; ok {
		counter.Add(1)
	}
}

// GenerationAdmissionDeniedCount returns the process-local denied counter for a domain.
func GenerationAdmissionDeniedCount(domain string) int64 {
	if counter, ok := generationAdmissionDenied[domain]; ok {
		return counter.Load()
	}
	return 0
}

func writeGenerationAdmissionDenied(b *strings.Builder) {
	b.WriteString("# HELP productflow_generation_admission_denied_total Generation admission checks that found capacity full.\n")
	b.WriteString("# TYPE productflow_generation_admission_denied_total counter\n")
	for _, domain := range generationAdmissionDomains {
		fmt.Fprintf(b, "productflow_generation_admission_denied_total{domain=%q} %d\n", domain, generationAdmissionDenied[domain].Load())
	}
}
