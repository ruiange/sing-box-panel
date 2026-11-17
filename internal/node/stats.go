package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node/v2raywire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	statsService  = "/v2ray.core.app.stats.command.StatsService/"
	sampleTimeout = 4 * time.Second
	probeTimeout  = 750 * time.Millisecond
)

type Counters struct {
	Upload, Download int64
}

type Snapshot struct {
	// sing-box reports uptime in whole seconds, so StartedAt is approximate.
	StartedAt time.Time
	ReadAt    time.Time
	Counters  map[string]Counters
}

type startWindow struct {
	earliest, latest time.Time
}

func (n *Node) getStartWindow(ctx context.Context) (startWindow, error) {
	requestedAt := time.Now()
	var response v2raywire.SysStatsResponse
	if err := n.conn.Invoke(ctx, statsService+"GetSysStats", &v2raywire.SysStatsRequest{}, &response); err != nil {
		return startWindow{}, fmt.Errorf("读取 sing-box 统计服务运行时间: %w", err)
	}
	receivedAt := time.Now()
	uptime := time.Duration(response.GetUptime()) * time.Second
	// The server rounded down its age at some time during the RPC.
	return startWindow{
		earliest: requestedAt.Add(-uptime - time.Second),
		latest:   receivedAt.Add(-uptime),
	}, nil
}

func (n *Node) collect(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, sampleTimeout)
	defer cancel()

	before, err := n.getStartWindow(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	requestedAt := time.Now()
	var response v2raywire.QueryStatsResponse
	request := &v2raywire.QueryStatsRequest{Patterns: []string{"user>>>" + counterUserPrefix}}
	if err := n.conn.Invoke(ctx, statsService+"QueryStats", request, &response); err != nil {
		return Snapshot{}, fmt.Errorf("读取 sing-box 流量计数: %w", err)
	}
	readAt := requestedAt.Add(time.Since(requestedAt) / 2)

	after, err := n.getStartWindow(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	earliest := maxTime(before.earliest, after.earliest)
	latest := minTime(before.latest, after.latest)
	if latest.Before(earliest) {
		return Snapshot{}, errors.New("流量采集期间 sing-box 统计服务已重建")
	}
	counters, err := parseCounters(response.GetStat())
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		StartedAt: earliest.Add(latest.Sub(earliest) / 2),
		ReadAt:    readAt,
		Counters:  counters,
	}, nil
}

func parseCounters(stats []*v2raywire.Stat) (map[string]Counters, error) {
	counters := make(map[string]Counters)
	seen := make(map[string]bool)
	for _, stat := range stats {
		name := stat.GetName()
		suffix, ok := strings.CutPrefix(name, "user>>>"+counterUserPrefix)
		if !ok {
			continue
		}
		id, direction, ok := strings.Cut(suffix, ">>>traffic>>>")
		if !ok || !deviceIDPattern.MatchString(id) {
			continue
		}
		if direction != "uplink" && direction != "downlink" {
			continue
		}
		value := stat.GetValue()
		if value < 0 || seen[name] {
			return nil, errors.New("sing-box 返回无效或重复的设备流量计数")
		}
		seen[name] = true
		current := counters[id]
		if direction == "uplink" {
			current.Upload = value
		} else {
			current.Download = value
		}
		counters[id] = current
	}
	return counters, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func minTime(a, b time.Time) time.Time {
	if a.After(b) {
		return b
	}
	return a
}

func newStatsConnection(address string) (*grpc.ClientConn, error) {
	// The target is a validated loopback IP. Do not consult proxy environment
	// variables for an unauthenticated local management API.
	return grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
}
