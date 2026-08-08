package hostmetrics

import "testing"

func TestCollectDoesNotPanic(t *testing.T) {
	metrics := Collect()
	if metrics.CollectedAt.IsZero() {
		t.Fatal("expected collectedAt")
	}
	// Second call warms Windows CPU sampling.
	_ = Collect()
}
