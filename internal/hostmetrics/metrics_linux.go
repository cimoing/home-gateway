//go:build linux

package hostmetrics

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Collect returns Linux host metrics from /proc and thermal sysfs.
func Collect() Metrics {
	now := time.Now().UTC()
	metrics := Metrics{CollectedAt: now}
	if load, ok := readLoad1(); ok {
		metrics.CPULoad = ptr(load)
	}
	if temp, ok := readCPUTempC(); ok {
		metrics.CPUTempC = ptr(temp)
	}
	if pressure, ok := readCPUPressure(); ok {
		metrics.CPUPressure = ptr(pressure)
	}
	if used, total, ok := readMemory(); ok && total > 0 {
		metrics.MemoryUsed = used
		metrics.MemoryTotal = total
		metrics.MemoryPercent = float64(used) * 100 / float64(total)
	}
	return metrics
}

func readLoad1() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readMemory() (used, total uint64, ok bool) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotal = parseMeminfoKB(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvailable = parseMeminfoKB(line)
		}
		if memTotal > 0 && memAvailable > 0 {
			break
		}
	}
	if memTotal == 0 {
		return 0, 0, false
	}
	total = memTotal * 1024
	if memAvailable > memTotal {
		memAvailable = memTotal
	}
	used = (memTotal - memAvailable) * 1024
	return used, total, true
}

func parseMeminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func readCPUPressure() (float64, bool) {
	data, err := os.ReadFile("/proc/pressure/cpu")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if !strings.HasPrefix(field, "avg10=") {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimPrefix(field, "avg10="), 64)
			if err != nil {
				return 0, false
			}
			return value, true
		}
	}
	return 0, false
}

func readCPUTempC() (float64, bool) {
	matches, err := filepath.Glob("/sys/class/thermal/thermal_zone*/type")
	if err != nil {
		return 0, false
	}
	preferred := []string{
		"x86_pkg_temp", "cpu-thermal", "soc_thermal", "cpu_thermal", "acpitz",
	}
	zones := make(map[string]string, len(matches))
	for _, typePath := range matches {
		name, err := os.ReadFile(typePath)
		if err != nil {
			continue
		}
		zones[strings.TrimSpace(string(name))] = filepath.Dir(typePath)
	}
	for _, key := range preferred {
		if dir, ok := zones[key]; ok {
			if temp, ok := readThermalMillidegC(filepath.Join(dir, "temp")); ok {
				return temp, true
			}
		}
	}
	for _, dir := range zones {
		if temp, ok := readThermalMillidegC(filepath.Join(dir, "temp")); ok {
			return temp, true
		}
	}
	return 0, false
}

func readThermalMillidegC(path string) (float64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	raw, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil || raw <= 0 {
		return 0, false
	}
	// Most thermal zones report millidegree Celsius.
	if raw > 1000 {
		return raw / 1000, true
	}
	return raw, true
}
