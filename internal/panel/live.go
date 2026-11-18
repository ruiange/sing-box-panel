package panel

import (
	"context"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ruiange/sing-box-panel/internal/node"
)

const (
	liveInterval       = 2 * time.Second
	liveWindow         = time.Minute
	liveMaxGap         = 6 * time.Second
	liveWriteTimeout   = 5 * time.Second
	livePingInterval   = 25 * time.Second
	liveReadTimeout    = 60 * time.Second
	liveSessionExpired = 4001
	startTimeTolerance = 2 * time.Second // StatsService reports uptime in whole seconds.
)

type speed struct {
	Upload   float64 `json:"upload"`
	Download float64 `json:"download"`
}

type speedPoint struct {
	At time.Time `json:"at"`
	speed
}

type liveSample struct {
	at      time.Time
	devices map[string]speed
}

type devicePresence struct {
	State        string     `json:"state"`
	LastActiveAt *time.Time `json:"lastActiveAt"`
}

type liveFrame struct {
	At       *time.Time                `json:"at"`
	Devices  map[string]speed          `json:"devices"`
	DeviceID string                    `json:"deviceId"`
	History  []speedPoint              `json:"history"`
	Presence map[string]devicePresence `json:"presence"`
}

type liveTraffic struct {
	mu             sync.Mutex
	startedAt      time.Time
	previous       node.Snapshot
	current        liveSample
	history        []liveSample
	lastActive     map[string]time.Time
	activityToSave map[string]time.Time
	updated        chan struct{}
	stopped        chan struct{}
	stopOnce       sync.Once
}

func (l *liveTraffic) restoreActivity(activity map[string]time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	maps.Copy(l.lastActive, activity)
}

func (l *liveTraffic) pendingActivity() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.activityToSave)
}

func (l *liveTraffic) markActivityPersisted() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.activityToSave)
}

func (l *liveTraffic) invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.previous.ReadAt.IsZero() {
		return
	}
	l.previous = node.Snapshot{}
	l.startedAt = time.Time{}
	l.current = liveSample{}
	l.notifyLocked()
}

func (l *liveTraffic) record(snapshot node.Snapshot, activeIDs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := l.previous
	elapsed := snapshot.ReadAt.Sub(previous.ReadAt)
	newGeneration := l.startedAt.IsZero() || snapshot.StartedAt.Sub(l.startedAt).Abs() > startTimeTolerance
	if !newGeneration && elapsed < time.Second/2 {
		return
	}
	l.previous = snapshot
	l.current = liveSample{}
	if newGeneration {
		l.startedAt = snapshot.StartedAt
	}
	for len(l.history) > 0 && l.history[0].at.Before(snapshot.ReadAt.Add(-liveWindow)) {
		l.history[0] = liveSample{}
		l.history = l.history[1:]
	}
	if !newGeneration && elapsed <= liveMaxGap {
		rates := make(map[string]speed, len(activeIDs))
		for _, id := range activeIDs {
			before, after := previous.Counters[id], snapshot.Counters[id]
			if after.Upload >= before.Upload && after.Download >= before.Download {
				rates[id] = speed{
					Upload:   float64(after.Upload-before.Upload) / elapsed.Seconds(),
					Download: float64(after.Download-before.Download) / elapsed.Seconds(),
				}
				if before != after && snapshot.ReadAt.After(l.lastActive[id]) {
					at := snapshot.ReadAt.UTC()
					l.lastActive[id] = at
					l.activityToSave[id] = at
				}
			}
		}
		l.current = liveSample{at: snapshot.ReadAt.UTC(), devices: rates}
		l.history = append(l.history, l.current)
	}
	l.notifyLocked()
}

func (l *liveTraffic) notifyLocked() {
	close(l.updated)
	l.updated = make(chan struct{})
}

func (l *liveTraffic) frame(id string, now time.Time) (liveFrame, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	frame := liveFrame{
		Devices: map[string]speed{}, DeviceID: id, History: []speedPoint{},
		Presence: make(map[string]devicePresence, len(l.lastActive)),
	}
	cutoff := now.Add(-liveWindow)
	for deviceID, last := range l.lastActive {
		frame.Presence[deviceID] = devicePresence{State: "unknown", LastActiveAt: &last}
	}
	if sample := l.current; !sample.at.IsZero() && now.Sub(sample.at) <= liveMaxGap {
		frame.At = &sample.at
		frame.Devices = sample.devices
		for deviceID := range sample.devices {
			presence := frame.Presence[deviceID]
			presence.State = "idle"
			if presence.LastActiveAt != nil && !presence.LastActiveAt.Before(cutoff) {
				presence.State = "active"
			}
			frame.Presence[deviceID] = presence
		}
	}
	if id != "" {
		for _, sample := range l.history {
			if rate, exists := sample.devices[id]; exists && !sample.at.Before(cutoff) {
				frame.History = append(frame.History, speedPoint{At: sample.at, speed: rate})
			}
		}
	}
	return frame, l.updated
}

func (s *Service) CloseLiveTraffic() {
	s.live.stopOnce.Do(func() { close(s.live.stopped) })
}

func (s *Service) liveHandler(auth *authenticator) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: liveWriteTimeout,
		CheckOrigin: func(r *http.Request) bool {
			values := r.Header.Values("Origin")
			return len(values) == 1 && values[0] != "" && s.allowsOrigin(r, values[0])
		},
		Error: func(w http.ResponseWriter, _ *http.Request, status int, _ error) {
			fail(w, status, "无法建立实时流量连接")
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		subscriptions := make(chan string, 1)
		go readLiveSubscriptions(ctx, cancel, conn, subscriptions)

		ping := time.NewTicker(livePingInterval)
		defer ping.Stop()
		deviceID := ""
		for {
			if !auth.authenticated(r) {
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(liveSessionExpired, "登录已失效"), time.Now().Add(liveWriteTimeout))
				return
			}
			frame, updated := s.live.frame(deviceID, time.Now())
			_ = conn.SetWriteDeadline(time.Now().Add(liveWriteTimeout))
			if err := conn.WriteJSON(response{Data: frame, Message: "success"}); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-s.live.stopped:
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "面板正在重启"), time.Now().Add(liveWriteTimeout))
				return
			case deviceID = <-subscriptions:
			case <-updated:
			case <-ping.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(liveWriteTimeout)); err != nil {
					return
				}
			}
		}
	}
}

func readLiveSubscriptions(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, subscriptions chan<- string) {
	defer cancel()
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(liveReadTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(liveReadTimeout))
	})
	for {
		var request struct {
			Type     string `json:"type"`
			DeviceID string `json:"deviceId"`
		}
		if err := conn.ReadJSON(&request); err != nil {
			return
		}
		id, err := uuid.Parse(request.DeviceID)
		if request.Type != "subscribe" || (request.DeviceID != "" && (err != nil || id.String() != request.DeviceID)) {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "无效的订阅请求"), time.Now().Add(liveWriteTimeout))
			return
		}
		select {
		case subscriptions <- request.DeviceID:
		case <-ctx.Done():
			return
		}
	}
}
