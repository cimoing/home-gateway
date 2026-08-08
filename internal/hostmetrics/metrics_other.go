//go:build !linux && !windows

package hostmetrics

import "time"

// Collect returns a timestamped empty snapshot on unsupported platforms.
func Collect() Metrics {
	return Metrics{CollectedAt: time.Now().UTC()}
}
