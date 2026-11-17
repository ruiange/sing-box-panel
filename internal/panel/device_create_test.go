package panel

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

func TestCreateDeviceRetryAfterResponseLost(t *testing.T) {
	const id = "90bac3e4-7e70-4b0a-9b44-0be57d0d4261"
	const body = `{"name":"Windows","requestId":"` + id + `"}`
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	type application struct {
		device      node.Device
		requestDone <-chan struct{}
	}
	applyStarted := make(chan application, 1)
	applyRelease := make(chan struct{})
	releaseApply := sync.OnceFunc(func() { close(applyRelease) })
	defer releaseApply()
	var applyCalls atomic.Int32
	managed := deviceCreateTestNode{apply: func(ctx context.Context, devices []node.Device) error {
		if applyCalls.Add(1) != 1 {
			return nil
		}
		if len(devices) != 1 {
			return errors.New("first apply did not contain one device")
		}
		requestDone, _ := ctx.Value(deviceCreateRequestDone{}).(<-chan struct{})
		applyStarted <- application{device: devices[0], requestDone: requestDone}
		select {
		case <-applyRelease:
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	client, endpoint := deviceCreateTestAPI(t, ctx, managed)
	requestPanel(t, ctx, client, http.MethodPost, endpoint, `{"name":"Windows","requestId":"`+strings.ToUpper(id)+`"}`, http.StatusBadRequest)
	requestPanel(t, ctx, client, http.MethodPost, endpoint, `{"name":"Windows","requestId":"not-a-uuid"}`, http.StatusBadRequest)
	lostCtx, disconnect := context.WithCancel(ctx)
	defer disconnect()
	request, err := http.NewRequestWithContext(lostCtx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	clientError := make(chan error, 1)
	go func() {
		result, err := client.Do(request)
		if result != nil {
			result.Body.Close()
		}
		clientError <- err
	}()
	var first application
	select {
	case first = <-applyStarted:
		if first.device.ID != id || first.device.UUID == id {
			t.Fatal("node should apply the requested device ID with a separate credential")
		}
	case <-ctx.Done():
		t.Fatal("node apply did not start before the deadline")
	}
	disconnect()
	select {
	case err := <-clientError:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected to lose the response, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("canceled client did not return before the deadline")
	}
	select {
	case <-first.requestDone:
	case <-ctx.Done():
		t.Fatal("server did not observe the lost connection before the deadline")
	}
	releaseApply()
	devices := panelData[[]store.Device](t, requestPanel(t, ctx, client, http.MethodGet, endpoint, "", http.StatusOK))
	if len(devices) != 1 || devices[0].ID != id || !devices[0].Enabled {
		t.Fatalf("device was not created after the client disconnected: %+v", devices)
	}
	replayed := panelData[store.Device](t, requestPanel(t, ctx, client, http.MethodPost, endpoint, body, http.StatusOK))
	if replayed.ID != id || replayed.Name != devices[0].Name || !replayed.CreatedAt.Equal(devices[0].CreatedAt) || applyCalls.Load() != 1 {
		t.Fatalf("replaying the request did not reuse the original result: %+v", replayed)
	}
	requestPanel(t, ctx, client, http.MethodPost, endpoint, `{"name":"Different","requestId":"`+id+`"}`, http.StatusConflict)
	if applyCalls.Load() != 1 {
		t.Fatal("conflicting request applied a node configuration")
	}

	const secondID = "ddb70c46-7ce9-4e5a-ad51-08929bebaa93"
	second := panelData[store.Device](t, requestPanel(t, ctx, client, http.MethodPost, endpoint,
		`{"name":"Windows","requestId":"`+secondID+`"}`, http.StatusCreated))
	legacy := panelData[store.Device](t, requestPanel(t, ctx, client, http.MethodPost, endpoint,
		`{"name":"Windows"}`, http.StatusCreated))
	legacyID, err := uuid.Parse(legacy.ID)
	if err != nil || legacyID.Version() != 4 || legacy.ID == id || legacy.ID == secondID || second.ID != secondID {
		t.Fatal("distinct requests and legacy clients must create distinct device IDs")
	}
	devices = panelData[[]store.Device](t, requestPanel(t, ctx, client, http.MethodGet, endpoint, "", http.StatusOK))
	if len(devices) != 3 || applyCalls.Load() != 3 {
		t.Fatalf("expected three independent creations, got %d devices and %d node applications", len(devices), applyCalls.Load())
	}
}

func TestCreateDeviceCanRetryAfterRollback(t *testing.T) {
	const body = `{"name":"Windows","requestId":"90bac3e4-7e70-4b0a-9b44-0be57d0d4261"}`
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var applyCalls atomic.Int32
	managed := deviceCreateTestNode{apply: func(_ context.Context, _ []node.Device) error {
		if applyCalls.Add(1) == 1 {
			return errors.New("simulated node apply failure")
		}
		return nil
	}}
	client, endpoint := deviceCreateTestAPI(t, ctx, managed)
	requestPanel(t, ctx, client, http.MethodPost, endpoint, body, http.StatusInternalServerError)
	devices := panelData[[]store.Device](t, requestPanel(t, ctx, client, http.MethodGet, endpoint, "", http.StatusOK))
	if len(devices) != 0 {
		t.Fatalf("failed create left an enabled record: %+v", devices)
	}
	created := panelData[store.Device](t, requestPanel(t, ctx, client, http.MethodPost, endpoint, body, http.StatusCreated))
	replayed := panelData[store.Device](t, requestPanel(t, ctx, client, http.MethodPost, endpoint, body, http.StatusOK))
	if created.ID == "" || created.ID != replayed.ID || applyCalls.Load() != 3 {
		t.Fatalf("rollback retry was not applied once: %d applications", applyCalls.Load())
	}
}

type deviceCreateTestNode struct {
	apply func(context.Context, []node.Device) error
}

type deviceCreateRequestDone struct{}

func (n deviceCreateTestNode) Apply(ctx context.Context, devices []node.Device) (bool, error) {
	err := n.apply(ctx, devices)
	return err == nil, err
}

func (deviceCreateTestNode) Collect(context.Context) (node.Snapshot, error) {
	now := time.Now()
	return node.Snapshot{ReadAt: now, StartedAt: now.Add(-time.Hour)}, nil
}

func deviceCreateTestAPI(t *testing.T, ctx context.Context, managed managedNode) (*http.Client, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	const password = "integration-password"
	service := NewService(db, managed, Config{Zone: time.UTC, Password: password}, slog.Default())
	router := service.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), deviceCreateRequestDone{}, r.Context().Done())
		router.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	requestPanel(t, ctx, client, http.MethodPost, server.URL+"/api/auth/login", `{"password":"`+password+`"}`, http.StatusOK)
	return client, server.URL + "/api/devices"
}
