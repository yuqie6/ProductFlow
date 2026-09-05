package main

import "testing"

func TestRunRejectsMalformedInputFlagsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"--product-inputs"}, {"--n", "invalid"}, {"--seed", "invalid"}, {"unexpected"}} {
		if got := runLive(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}

func TestPrepareRequiresOutputBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{nil, {"--out"}, {"--out", ""}, {"--out", "unused.json", "--n", "invalid"}} {
		if got := runPrepareInputs(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}
