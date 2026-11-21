package panel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelPageRoutes(t *testing.T) {
	dir := t.TempDir()
	const index = `<!doctype html><html><title>Panel fixture</title></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	const asset = "body { color: black }"
	if err := os.WriteFile(filepath.Join(dir, "assets", "panel.css"), []byte(asset), 0600); err != nil {
		t.Fatal(err)
	}
	handler := (&Service{cfg: Config{WebDir: dir}}).Handler()
	get := func(route string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(http.MethodGet, route, nil))
		return result
	}

	for _, route := range []string{
		"/", "/m", "/m/",
		"/overview", "/overview/", "/m/overview", "/m/overview/",
		"/devices", "/devices/", "/m/devices", "/m/devices/",
		"/traffic", "/traffic/", "/m/traffic", "/m/traffic/?deviceId=phone%2F1",
		"/analysis", "/analysis/", "/m/analysis", "/m/analysis/",
		"/access", "/access/", "/m/access", "/m/access/",
	} {
		t.Run(route, func(t *testing.T) {
			result := get(route)
			if result.Code != http.StatusOK || result.Body.String() != index ||
				!strings.HasPrefix(result.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("%s did not serve the panel: status=%d, content type=%q, body=%q",
					route, result.Code, result.Header().Get("Content-Type"), result.Body)
			}
		})
	}

	for _, route := range []string{"/docs", "/m/docs", "/m/docs/?deviceId=phone%2F1"} {
		result := get(route)
		if result.Code != http.StatusOK || result.Body.String() != scalarHTML {
			t.Errorf("%s did not serve Scalar: status=%d, body=%q", route, result.Code, result.Body)
		}
	}
	if result := get("/assets/panel.css"); result.Code != http.StatusOK || result.Body.String() != asset {
		t.Errorf("asset did not retain its response: status=%d, body=%q", result.Code, result.Body)
	}
	if result := get("/api/missing"); result.Code != http.StatusNotFound ||
		!strings.HasPrefix(result.Header().Get("Content-Type"), "application/json") {
		t.Errorf("API 404 did not retain its JSON response: status=%d, body=%q", result.Code, result.Body)
	}
	if result := get("/sub/invalid/clash"); result.Code != http.StatusNotFound ||
		result.Header().Get("Cache-Control") != "no-store, private" {
		t.Errorf("subscription 404 did not retain the subscription headers: status=%d, headers=%v", result.Code, result.Header())
	}
	for _, route := range []string{"/m/typo", "/m/docs/typo", "/m/traffic/typo", "/devices//", "/typo", "/assets/unknown.css"} {
		result := get(route)
		if result.Code != http.StatusNotFound {
			t.Errorf("%s returned status %d, want 404", route, result.Code)
		}
	}
}

func TestPanelPageRoutesWithoutWebBuild(t *testing.T) {
	handler := (&Service{cfg: Config{WebDir: t.TempDir()}}).Handler()
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/m/traffic", nil))
	if result.Code != http.StatusServiceUnavailable {
		t.Errorf("missing web build returned status %d, want 503", result.Code)
	}
}
