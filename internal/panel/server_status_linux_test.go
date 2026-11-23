package panel

import (
	"strings"
	"testing"
)

func TestParseServerCPU(t *testing.T) {
	stat := "cpu  100 20 30 300 50 15 5 10 8 3\ncpu0 40 10 15 100 25 5 0 5 4 0\ncpu3 60 10 15 200 25 10 5 5 4 3\nintr 0"
	ticks, cores := parseServerCPU(strings.NewReader(stat))
	if ticks.total != 530 || ticks.idle != 350 || cores != 2 {
		t.Errorf("读取 CPU 计数和在线核心数得到 %+v / %d，期望 530、350 和 2", ticks, cores)
	}
}

func TestParseServerMemory(t *testing.T) {
	memory := parseServerMemory("MemAvailable:        256 kB\nMemFree:      128 kB\nMemTotal:           1024 kB\n")
	if memory == nil || memory.TotalBytes != 1048576 || memory.UsedBytes != 786432 || memory.Percent != 75 {
		t.Fatalf("内存使用量为 %+v，期望 75%%（786432 / 1048576 字节）", memory)
	}
	if memory := parseServerMemory("MemTotal:            1024 kB\nMemFree:      128 kB\n"); memory != nil {
		t.Fatalf("缺少 MemAvailable 时不应返回内存使用量，实际为 %+v", memory)
	}
}
