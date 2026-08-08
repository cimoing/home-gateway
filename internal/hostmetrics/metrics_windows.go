//go:build windows

package hostmetrics

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modKernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = modKernel32.NewProc("GetSystemTimes")

	cpuSampleMu   sync.Mutex
	cpuSampleIdle uint64
	cpuSampleTotal uint64
	cpuSampleReady bool
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// Collect returns Windows host metrics. Temperature and PSI are unavailable.
func Collect() Metrics {
	now := time.Now().UTC()
	metrics := Metrics{CollectedAt: now}
	if used, total, percent, ok := readMemory(); ok {
		metrics.MemoryUsed = used
		metrics.MemoryTotal = total
		metrics.MemoryPercent = percent
	}
	if load, ok := readCPULoad(); ok {
		metrics.CPULoad = ptr(load)
	}
	return metrics
}

func readMemory() (used, total uint64, percent float64, ok bool) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 || status.TotalPhys == 0 {
		return 0, 0, 0, false
	}
	total = status.TotalPhys
	if status.AvailPhys > total {
		status.AvailPhys = total
	}
	used = total - status.AvailPhys
	percent = float64(status.MemoryLoad)
	if percent <= 0 && total > 0 {
		percent = float64(used) * 100 / float64(total)
	}
	return used, total, percent, true
}

func readCPULoad() (float64, bool) {
	firstIdle, firstKernel, firstUser, ok := systemTimes()
	if !ok {
		return 0, false
	}
	firstTotal := firstKernel + firstUser

	cpuSampleMu.Lock()
	prevIdle := cpuSampleIdle
	prevTotal := cpuSampleTotal
	ready := cpuSampleReady
	if !ready {
		cpuSampleIdle = firstIdle
		cpuSampleTotal = firstTotal
		cpuSampleReady = true
	}
	cpuSampleMu.Unlock()

	idle := firstIdle
	total := firstTotal
	if !ready {
		time.Sleep(120 * time.Millisecond)
		var kernel, user uint64
		idle, kernel, user, ok = systemTimes()
		if !ok {
			return 0, false
		}
		total = kernel + user
		prevIdle = firstIdle
		prevTotal = firstTotal
	}

	idleDelta := idle - prevIdle
	totalDelta := total - prevTotal
	cpuSampleMu.Lock()
	cpuSampleIdle = idle
	cpuSampleTotal = total
	cpuSampleMu.Unlock()
	if totalDelta == 0 {
		return 0, false
	}
	busy := 1 - float64(idleDelta)/float64(totalDelta)
	if busy < 0 {
		busy = 0
	}
	if busy > 1 {
		busy = 1
	}
	// Approximate load average using utilization × logical CPUs.
	cpus := float64(windows.GetActiveProcessorCount(windows.ALL_PROCESSOR_GROUPS))
	if cpus <= 0 {
		cpus = 1
	}
	return busy * cpus, true
}

func systemTimes() (idle, kernel, user uint64, ok bool) {
	var idleTime, kernelTime, userTime windows.Filetime
	ret, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return 0, 0, 0, false
	}
	return filetimeToUint64(idleTime), filetimeToUint64(kernelTime), filetimeToUint64(userTime), true
}

func filetimeToUint64(value windows.Filetime) uint64 {
	return (uint64(value.HighDateTime) << 32) | uint64(value.LowDateTime)
}
