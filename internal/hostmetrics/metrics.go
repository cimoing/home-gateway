package hostmetrics

import "time"

// Metrics is a point-in-time host resource snapshot for the UI header.
type Metrics struct {
	CPULoad       *float64  `json:"cpuLoad"`
	CPUTempC      *float64  `json:"cpuTempC"`
	CPUPressure   *float64  `json:"cpuPressure"`
	MemoryPercent float64   `json:"memoryPercent"`
	MemoryUsed    uint64    `json:"memoryUsedBytes"`
	MemoryTotal   uint64    `json:"memoryTotalBytes"`
	CollectedAt   time.Time `json:"collectedAt"`
}

func ptr[T any](value T) *T {
	return &value
}
