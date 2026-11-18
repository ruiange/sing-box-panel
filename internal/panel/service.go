package panel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

type managedNode interface {
	Apply(context.Context, []node.Device) (bool, error)
	Collect(context.Context) (node.Snapshot, error)
}

type Service struct {
	store  *store.Store
	node   managedNode
	cfg    Config
	log    *slog.Logger
	access *domainAccess

	mu            sync.Mutex // Serializes accounting samples and changes to the node.
	status        string
	activeIDs     []string
	lastPersisted time.Time
	live          liveTraffic
}

func NewService(db *store.Store, managed managedNode, cfg Config, logger *slog.Logger) *Service {
	return &Service{
		store: db, node: managed, cfg: cfg, log: logger, status: "waiting",
		live: liveTraffic{
			updated: make(chan struct{}), stopped: make(chan struct{}),
			lastActive: make(map[string]time.Time), activityToSave: make(map[string]time.Time),
		},
	}
}

func (s *Service) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	activity, err := s.store.LastDeviceActivity(ctx)
	if err != nil {
		return fmt.Errorf("读取设备最后活跃时间: %w", err)
	}
	s.live.restoreActivity(activity)
	if s.node == nil {
		return nil
	}
	// Preserve the last counters if the core survived a panel crash.
	// A fresh install has no service to sample yet.
	if err := s.collectLocked(ctx); err != nil {
		s.status = "waiting"
	}
	return s.applyLocked(ctx)
}

func (s *Service) RunCollector(ctx context.Context) {
	if s.node == nil {
		return
	}
	ticker := time.NewTicker(min(s.cfg.PollInterval, liveInterval))
	defer ticker.Stop()
	var lastWarning time.Time
	for {
		s.mu.Lock()
		err := s.sampleLocked(ctx, false)
		s.mu.Unlock()
		if err != nil && ctx.Err() == nil && time.Since(lastWarning) >= s.cfg.PollInterval {
			lastWarning = time.Now()
			s.log.Warn("采集流量失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) CollectBeforeShutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.collectLocked(ctx)
}

func (s *Service) collectLocked(ctx context.Context) error {
	return s.sampleLocked(ctx, true)
}

func (s *Service) sampleLocked(ctx context.Context, persist bool) error {
	if s.node == nil {
		return nil
	}
	snapshot, err := s.node.Collect(ctx)
	if err != nil {
		s.live.invalidate()
		s.status = "error"
		return err
	}
	s.live.record(snapshot, s.activeIDs)
	if !persist && snapshot.ReadAt.Sub(s.lastPersisted) < s.cfg.PollInterval {
		s.status = "ok"
		return nil
	}
	stats := make(map[string]store.Counters, len(snapshot.Counters))
	for id, counters := range snapshot.Counters {
		stats[id] = store.Counters{Upload: counters.Upload, Download: counters.Download}
	}
	activity := s.live.pendingActivity()
	if err := s.store.RecordTrafficWithActivity(ctx, snapshot.ReadAt, snapshot.StartedAt, stats, s.cfg.Zone, activity); err != nil {
		s.status = "error"
		return err
	}
	s.live.markActivityPersisted()
	s.lastPersisted = snapshot.ReadAt
	s.status = "ok"
	return nil
}

func (s *Service) applyLocked(ctx context.Context) error {
	if s.node == nil {
		return nil
	}
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return err
	}
	active := make([]node.Device, 0, len(devices))
	for _, device := range devices {
		if device.Enabled {
			active = append(active, node.Device{ID: device.ID, UUID: device.UUID})
		}
	}
	reloaded, applyErr := s.node.Apply(ctx, active)
	if applyErr == nil {
		s.activeIDs = make([]string, 0, len(active))
		for _, device := range active {
			s.activeIDs = append(s.activeIDs, device.ID)
		}
	}
	if reloaded {
		s.live.invalidate()
		s.status = "waiting"
		return errors.Join(applyErr, s.store.ResetTrafficBaseline(ctx))
	}
	return applyErr
}

func (s *Service) changeDevice(ctx context.Context, before, after store.Device, isNew bool) error {
	needsApply := s.node != nil && (isNew || before.Enabled != after.Enabled)
	// The client may disconnect mid-change. Finish the bounded write/apply/rollback independently.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	if needsApply {
		if err := s.collectLocked(ctx); err != nil {
			if isNew || after.Enabled {
				return fmt.Errorf("配置变更前无法保存流量: %w", err)
			}
			s.log.Warn("停用设备前无法保存最新流量，继续撤销凭据", "error", err)
		}
	}
	if isNew {
		if err := s.store.CreateDevice(ctx, after); err != nil {
			return err
		}
	} else if err := s.store.UpdateDevice(ctx, after); err != nil {
		return err
	}
	if !needsApply {
		return nil
	}
	if err := s.applyLocked(ctx); err != nil {
		recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
		defer cancelRecovery()
		var rollbackErr error
		if isNew {
			rollbackErr = s.store.DeleteDevice(recoveryCtx, after.ID)
		} else {
			rollbackErr = s.store.UpdateDevice(recoveryCtx, before)
		}
		if rollbackErr == nil {
			rollbackErr = s.applyLocked(recoveryCtx)
		}
		if rollbackErr != nil {
			s.status = "error"
		}
		return errors.Join(err, rollbackErr)
	}
	return nil
}
