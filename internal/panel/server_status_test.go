package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestServerStatusRequiresSession(t *testing.T) {
	const endpoint = "/api/server/status"
	service := NewService(nil, nil, Config{Password: "test-password"}, nil)
	handler := service.Handler()

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, endpoint, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录时 HTTP 状态为 %d，期望 401", w.Code)
	}

	w = httptest.NewRecorder()
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"test-password"}`))
	handler.ServeHTTP(w, login)
	loginResult := w.Result()
	defer loginResult.Body.Close()
	cookies := loginResult.Cookies()
	if w.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("测试会话创建失败: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, endpoint, nil)
	request.AddCookie(cookies[0])
	handler.ServeHTTP(w, request)
	var result struct {
		Code    int                        `json:"code"`
		Data    map[string]json.RawMessage `json:"data"`
		Message string                     `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Code != 0 || result.Message == "" {
		t.Fatalf("服务器状态响应格式错误: %d %s (%v)", w.Code, w.Body, err)
	}
	for _, metric := range []string{"load", "cpu", "memory", "disk"} {
		value, exists := result.Data[metric]
		if !exists {
			t.Errorf("响应缺少指标 %s", metric)
		}
		if runtime.GOOS != "linux" && string(value) != "null" {
			t.Errorf("非 Linux 系统的指标 %s 应为 null，实际为 %s", metric, value)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("服务器状态的 Cache-Control = %q，期望 no-store", w.Header().Get("Cache-Control"))
	}
}

func TestCPUUsageFromCounterDeltas(t *testing.T) {
	previous := cpuTicks{total: 1000, idle: 800}
	current := cpuTicks{total: 1100, idle: 875}
	if got := current.usageSince(previous); got == nil || *got != 25 {
		t.Fatalf("CPU 使用率为 %v，期望 25%%", got)
	}
	for _, invalid := range []cpuTicks{{}, previous, {total: 999, idle: 800}, {total: 1100, idle: 799}, {total: 1100, idle: 901}} {
		if got := invalid.usageSince(previous); got != nil {
			t.Errorf("无法计算使用率的样本 %+v 返回了 %v", invalid, *got)
		}
	}
	if got := current.usageSince(cpuTicks{}); got != nil {
		t.Errorf("没有基线的样本返回了 %v", *got)
	}
}
