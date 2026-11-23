package panel

import (
	"bufio"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func readServerStatus() (serverStatus, cpuTicks) {
	result := serverStatus{Memory: readServerMemory(), Disk: readServerDisk()}
	stat, err := os.Open("/proc/stat")
	if err != nil {
		return result, cpuTicks{}
	}
	defer stat.Close()
	ticks, cores := parseServerCPU(stat)
	if cores > 0 {
		result.CPU = &serverCPU{Cores: cores}
		result.Load = readServerLoad(cores)
	}
	return result, ticks
}

func parseServerCPU(stat io.Reader) (cpuTicks, int) {
	lines := bufio.NewScanner(stat)
	if !lines.Scan() {
		return cpuTicks{}, 0
	}
	fields := strings.Fields(lines.Text())
	if len(fields) < 9 || fields[0] != "cpu" {
		return cpuTicks{}, 0
	}
	var ticks cpuTicks
	// The first eight counters include guest time in user and nice already.
	for i, field := range fields[1:9] {
		tick, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuTicks{}, 0
		}
		ticks.total += tick
		if i == 3 || i == 4 { // idle and iowait
			ticks.idle += tick
		}
	}
	var cores int
	for lines.Scan() {
		if !strings.HasPrefix(lines.Text(), "cpu") {
			break
		}
		cores++
	}
	return ticks, cores
}

func readServerLoad(cores int) *serverLoad {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return nil
	}
	average, err := strconv.ParseFloat(fields[0], 64)
	percent := average / float64(cores) * 100
	if err != nil || average < 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return nil
	}
	return &serverLoad{Percent: percent, Average1m: average}
}

func readServerMemory() *serverCapacity {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	return parseServerMemory(string(data))
}

func parseServerMemory(data string) *serverCapacity {
	var total, available uint64
	var foundAvailable bool
	for line := range strings.Lines(data) {
		key, value, _ := strings.Cut(line, ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != "kB" {
			return nil
		}
		kib, err := strconv.ParseUint(fields[0], 10, 64-10)
		if err != nil {
			return nil
		}
		if key == "MemTotal" {
			total = kib * 1024
		} else {
			available = kib * 1024
			foundAvailable = true
		}
		if total > 0 && foundAvailable {
			return capacityUsage(total, available)
		}
	}
	return nil
}

func readServerDisk() *serverCapacity {
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err != nil {
		return nil
	}
	blockSize := fs.Frsize
	if blockSize <= 0 {
		blockSize = fs.Bsize
	}
	if blockSize <= 0 || fs.Blocks > math.MaxUint64/uint64(blockSize) || fs.Bfree > fs.Blocks {
		return nil
	}
	return capacityUsage(fs.Blocks*uint64(blockSize), fs.Bfree*uint64(blockSize))
}

func capacityUsage(total, available uint64) *serverCapacity {
	if total == 0 || available > total {
		return nil
	}
	used := total - available
	return &serverCapacity{
		Percent: float64(used) / float64(total) * 100, UsedBytes: used, TotalBytes: total,
	}
}
