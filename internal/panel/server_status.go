package panel

import (
	"net/http"
	"sync"
	"time"
)

type serverStatus struct {
	Load   *serverLoad     `json:"load"`
	CPU    *serverCPU      `json:"cpu"`
	Memory *serverCapacity `json:"memory"`
	Disk   *serverCapacity `json:"disk"`
}

type serverLoad struct {
	Percent   float64 `json:"percent"`
	Average1m float64 `json:"average1m"`
}

type serverCPU struct {
	Percent *float64 `json:"percent"`
	Cores   int      `json:"cores"`
}

type serverCapacity struct {
	Percent    float64 `json:"percent"`
	UsedBytes  uint64  `json:"usedBytes"`
	TotalBytes uint64  `json:"totalBytes"`
}

type cpuTicks struct {
	total uint64
	idle  uint64
}

func (now cpuTicks) usageSince(previous cpuTicks) *float64 {
	if previous.total == 0 || now.total <= previous.total || now.idle < previous.idle {
		return nil
	}
	total, idle := now.total-previous.total, now.idle-previous.idle
	if idle > total {
		return nil
	}
	percent := float64(total-idle) / float64(total) * 100
	return &percent
}

const (
	serverStatusCacheTime  = time.Second
	serverStatusCPUMaxSpan = 30 * time.Second
)

type serverStatusSampler struct {
	mu        sync.Mutex
	sampledAt time.Time
	cpu       cpuTicks
	latest    serverStatus
}

func (s *serverStatusSampler) snapshot() serverStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(s.sampledAt)
	if elapsed < serverStatusCacheTime {
		return s.latest
	}
	result, cpu := readServerStatus()
	if result.CPU != nil && elapsed <= serverStatusCPUMaxSpan {
		result.CPU.Percent = cpu.usageSince(s.cpu)
	}
	s.sampledAt, s.cpu, s.latest = now, cpu, result
	return result
}

func (s *Service) getServerStatus(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, s.serverStatus.snapshot())
}
