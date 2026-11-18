package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ruiange/sing-box-panel/internal/store"
)

func (s *Service) Handler() http.Handler {
	r := chi.NewRouter()
	auth := newAuthenticator(s.cfg.Password, s.cfg.SecureCookie, s.cfg.TrustLocalProxy)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	})
	r.Route("/api", func(api chi.Router) {
		api.Use(s.protectAPI)
		api.Get("/auth/session", func(w http.ResponseWriter, r *http.Request) {
			respond(w, http.StatusOK, map[string]bool{"authenticated": auth.authenticated(r)})
		})
		api.Post("/auth/login", auth.login)
		api.Post("/auth/logout", auth.logout)
		api.Group(func(private chi.Router) {
			private.Use(auth.require)
			private.Get("/settings/access", s.getAccessSettings)
			private.Put("/settings/access", s.putAccessSettings)
			private.Get("/devices", s.listDevices)
			private.Post("/devices", s.createDevice)
			private.Patch("/devices/{id}", s.updateDevice)
			private.Get("/devices/{id}/destinations", s.deviceDestinations)
			private.Get("/devices/{id}/subscription", s.getSubscription)
			private.Get("/devices/{id}/subscription-qr", s.subscriptionQR)
			private.Post("/devices/{id}/rotate-subscription", s.rotateSubscription)
			private.Get("/traffic", s.traffic)
			private.Get("/traffic/live", s.liveHandler(auth))
		})
		api.NotFound(func(w http.ResponseWriter, _ *http.Request) { fail(w, http.StatusNotFound, "接口不存在") })
		api.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			fail(w, http.StatusMethodNotAllowed, "请求方法不支持")
		})
	})
	r.Get("/sub/{token}", s.subscription)
	r.Get("/sub/{token}/{format}", s.subscription)
	r.Get("/openapi.yaml", openAPIDocument)
	r.Get("/docs", scalarPage)
	files := http.FileServer(http.Dir(s.cfg.WebDir))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if path.Clean(r.URL.Path) != r.URL.Path || (r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/favicon.svg") {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(s.cfg.WebDir, "index.html")); err != nil {
			http.Error(w, "网页尚未构建。开发可以用 pnpm --dir web dev，接口文档见 /docs。", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
	return r
}

func (s *Service) protectAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" && !s.allowsOrigin(r, origin) {
				fail(w, http.StatusForbidden, "请求来源不允许")
				return
			}
			if r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, http.StatusForbidden, "请求来源不允许")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) allowsOrigin(r *http.Request, origin string) bool {
	if origin == s.cfg.DevOrigin && origin != "" {
		return true
	}
	if s.access != nil && r.TLS != nil {
		site := s.access.active.Load()
		return site != nil && origin == "https://"+site.domain
	}
	return origin == s.cfg.PublicURL
}

type response struct {
	Code    int    `json:"code"`
	Data    any    `json:"data"`
	Message string `json:"message"`
}

func respond(w http.ResponseWriter, status int, data any) {
	send(w, status, response{Data: data, Message: "success"})
}

func fail(w http.ResponseWriter, status int, message string) {
	send(w, status, response{Code: status, Message: message})
}

func send(w http.ResponseWriter, status int, value response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return readJSONLimit(w, r, dst, 4096)
}

func readJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "请求数据不符合格式")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}

func cleanName(value string) (string, bool) {
	name := strings.TrimSpace(value)
	length := utf8.RuneCountInString(name)
	return name, length >= 1 && length <= 48 && strings.IndexFunc(name, unicode.IsControl) < 0
}

func (s *Service) listDevices(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.report(w, "读取设备失败", err)
		return
	}
	respond(w, http.StatusOK, devices)
}

func (s *Service) createDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string `json:"name"`
		RequestID string `json:"requestId"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	name, valid := cleanName(input.Name)
	if !valid {
		fail(w, http.StatusBadRequest, "设备名称需为 1–48 个字符")
		return
	}
	if input.RequestID != "" {
		id, err := uuid.Parse(input.RequestID)
		if err != nil || id.String() != input.RequestID || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
			fail(w, http.StatusBadRequest, "设备创建请求标识需为小写 UUID v4")
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	deviceID := input.RequestID
	if deviceID != "" {
		existing, err := s.store.GetDevice(r.Context(), deviceID)
		switch {
		case err == nil:
			if existing.Name != name {
				fail(w, http.StatusConflict, "该创建请求标识已用于其他设备名称")
				return
			}
			if s.node != nil && existing.Enabled && !slices.Contains(s.activeIDs, existing.ID) {
				fail(w, http.StatusServiceUnavailable, "设备记录已存在，节点配置尚未确认生效")
				return
			}
			respond(w, http.StatusOK, existing)
			return
		case !errors.Is(err, store.ErrNotFound):
			s.report(w, "读取设备创建结果失败", err)
			return
		}
	} else {
		id, err := uuid.NewRandom()
		if err != nil {
			s.report(w, "生成设备标识失败", err)
			return
		}
		deviceID = id.String()
	}
	credential, credentialErr := uuid.NewRandom()
	token, tokenErr := randomToken()
	if err := errors.Join(credentialErr, tokenErr); err != nil {
		s.report(w, "生成设备凭据失败", err)
		return
	}
	device := store.Device{ID: deviceID, Name: name, Enabled: true, UUID: credential.String(), SubscriptionToken: token, CreatedAt: time.Now().UTC()}
	if err := s.changeDevice(r.Context(), store.Device{}, device, true); err != nil {
		s.report(w, "添加设备失败，请检查节点状态与服务日志", err)
		return
	}
	respond(w, http.StatusCreated, device)
}

func (s *Service) updateDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Name == nil && input.Enabled == nil {
		fail(w, http.StatusBadRequest, "请提供要更新的设备信息")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before, ok := s.device(w, r)
	if !ok {
		return
	}
	after := before
	if input.Name != nil {
		name, valid := cleanName(*input.Name)
		if !valid {
			fail(w, http.StatusBadRequest, "设备名称需为 1–48 个字符")
			return
		}
		after.Name = name
	}
	if input.Enabled != nil {
		after.Enabled = *input.Enabled
	}
	if after != before {
		if err := s.changeDevice(r.Context(), before, after, false); err != nil {
			s.report(w, "更新设备失败，请检查节点状态与服务日志", err)
			return
		}
	}
	respond(w, http.StatusOK, after)
}

func (s *Service) device(w http.ResponseWriter, r *http.Request) (store.Device, bool) {
	device, err := s.store.GetDevice(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "设备不存在")
	} else if err != nil {
		s.report(w, "读取设备失败", err)
	}
	return device, err == nil
}

func (s *Service) deviceDestinations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := store.MaxDestinationsPerPage
	if parameter := query.Get("limit"); parameter != "" {
		var err error
		limit, err = strconv.Atoi(parameter)
		if err != nil || limit < 1 || limit > store.MaxDestinationsPerPage {
			fail(w, http.StatusBadRequest, fmt.Sprintf("limit 必须是 1 至 %d 的整数", store.MaxDestinationsPerPage))
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.device(w, r)
	if !ok {
		return
	}
	page, err := s.store.ListDestinations(r.Context(), device.ID, limit, query.Get("before"))
	if errors.Is(err, store.ErrInvalidDestinationCursor) {
		fail(w, http.StatusBadRequest, "访问记录分页游标无效")
		return
	}
	if err != nil {
		s.report(w, "读取访问地址失败", err)
		return
	}
	respond(w, http.StatusOK, page)
}

func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.node == nil {
		fail(w, http.StatusServiceUnavailable, "尚未启用 sing-box 节点")
		return
	}
	if device, ok := s.device(w, r); ok {
		if !device.Enabled {
			fail(w, http.StatusConflict, "设备已停用")
			return
		}
		respond(w, http.StatusOK, map[string]string{"url": s.publicURL() + "/sub/" + device.SubscriptionToken})
	}
}

func (s *Service) rotateSubscription(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.node == nil {
		fail(w, http.StatusServiceUnavailable, "尚未启用 sing-box 节点")
		return
	}
	device, ok := s.device(w, r)
	if !ok {
		return
	}
	if !device.Enabled {
		fail(w, http.StatusConflict, "设备已停用")
		return
	}
	token, err := randomToken()
	if err != nil {
		s.report(w, "生成订阅链接失败", err)
		return
	}
	device.SubscriptionToken = token
	if err := s.store.UpdateDevice(r.Context(), device); err != nil {
		s.report(w, "轮换订阅链接失败", err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"url": s.publicURL() + "/sub/" + token})
}

func (s *Service) traffic(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	days := 7
	if parameter := query.Get("days"); parameter != "" {
		var err error
		days, err = strconv.Atoi(parameter)
		if err != nil || (days != 7 && days != 30) {
			fail(w, http.StatusBadRequest, "days 只能是 7 或 30")
			return
		}
	}
	var hours int
	if parameter := query.Get("hours"); parameter != "" {
		var err error
		hours, err = strconv.Atoi(parameter)
		if err != nil || (hours != 6 && hours != 12 && hours != 24 && hours != 48 && hours != 72) {
			fail(w, http.StatusBadRequest, "hours 只能是 6、12、24、48 或 72")
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	id := query.Get("deviceId")
	var usage store.Traffic
	var err error
	switch {
	case hours != 0:
		usage, err = s.store.HourlyTraffic(r.Context(), now, days, hours, s.cfg.Zone, id)
	case id != "":
		usage, err = s.store.DeviceTraffic(r.Context(), now, days, s.cfg.Zone, id)
	default:
		usage, err = s.store.Traffic(r.Context(), now, days, s.cfg.Zone)
	}
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "设备不存在")
		return
	}
	if err != nil {
		s.report(w, "查询流量失败", err)
		return
	}
	usage.CollectorStatus = s.status
	respond(w, http.StatusOK, usage)
}

func (s *Service) report(w http.ResponseWriter, message string, err error) {
	s.log.Error(message, "error", err)
	fail(w, http.StatusInternalServerError, message)
}
