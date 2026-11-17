package panel

import (
	"context"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

const destinationBatchSize = 256

func (s *Service) RunDestinationCollector(ctx context.Context, managed *node.Node) {
	updates := make(chan []node.ObservedConnection)
	go s.watchDestinations(ctx, managed, updates)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	pending := make([]store.DestinationEvent, 0, destinationBatchSize)
	var lastSaveWarning time.Time
	flush := func() bool {
		for len(pending) > 0 {
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			err := s.store.RecordDestinations(saveCtx, pending)
			cancel()
			if err == nil {
				pending = pending[:0]
				return true
			}
			if time.Since(lastSaveWarning) >= 30*time.Second {
				s.log.Warn("保存访问记录失败", "error", err)
				lastSaveWarning = time.Now()
			}
			// Pause stream consumption while storage is unavailable, keeping
			// the current batch for retry instead of overwriting observations.
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Second):
			}
		}
		return true
	}
	for {
		select {
		case batch, ok := <-updates:
			if !ok {
				flush()
				return
			}
			for _, event := range batch {
				pending = append(pending, store.DestinationEvent{
					ConnectionID: event.ConnectionID, DeviceID: event.DeviceID, Address: event.Address, SeenAt: event.SeenAt,
				})
				if len(pending) == destinationBatchSize && !flush() {
					return
				}
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		}
	}
}

func (s *Service) watchDestinations(ctx context.Context, managed *node.Node, updates chan<- []node.ObservedConnection) {
	defer close(updates)
	var lastWarning time.Time
	for ctx.Err() == nil {
		err := managed.WatchDestinations(ctx, func(batch []node.ObservedConnection) error {
			select {
			case updates <- batch:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if ctx.Err() != nil {
			return
		}
		if time.Since(lastWarning) >= 30*time.Second {
			s.log.Warn("访问记录采集连接断开", "error", err)
			lastWarning = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
