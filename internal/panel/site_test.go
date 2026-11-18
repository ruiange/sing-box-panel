package panel

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruiange/sing-box-panel/internal/store"
)

func TestSiteTitleRequiresLoginToChangeAndPersists(t *testing.T) {
	const endpoint = "/api/site"
	const defaultTitle = "趣多多流量冠军"
	const customTitle = "我的流量面板 🌐"
	dbPath := filepath.Join(t.TempDir(), "panel.db")

	withServer := func(check func(*httptest.Server)) {
		t.Helper()
		db, err := store.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		service := NewService(db, nil, Config{Password: "test-password"}, slog.Default())
		server := httptest.NewServer(service.Handler())
		defer server.Close()
		check(server)
	}
	readTitle := func(server *httptest.Server, want string) {
		t.Helper()
		data := requestPanel(t, t.Context(), server.Client(), http.MethodGet, server.URL+endpoint, "", http.StatusOK)
		if got := panelData[siteSettings](t, data).Title; got != want {
			t.Fatalf("read title %q; want %q", got, want)
		}
	}

	withServer(func(server *httptest.Server) {
		readTitle(server, defaultTitle)
		target := server.URL + endpoint
		requestPanel(t, t.Context(), server.Client(), http.MethodPut, target, `{"title":"未登录"}`, http.StatusUnauthorized)
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: server.Client().Transport, Jar: jar}
		requestPanel(t, t.Context(), client, http.MethodPost, server.URL+"/api/auth/login", `{"password":"test-password"}`, http.StatusOK)

		data := requestPanel(t, t.Context(), client, http.MethodPut, target, `{"title":"　`+customTitle+`　"}`, http.StatusOK)
		if got := panelData[siteSettings](t, data).Title; got != customTitle {
			t.Fatalf("saved title %q; want %q", got, customTitle)
		}
		for _, invalid := range []string{
			`{}`, `{"title":"　 "}`, `{"title":"第一行\n第二行"}`, `{"title":"` + strings.Repeat("🌐", 81) + `"}`,
		} {
			requestPanel(t, t.Context(), client, http.MethodPut, target, invalid, http.StatusBadRequest)
		}
		readTitle(server, customTitle)
	})

	withServer(func(server *httptest.Server) {
		readTitle(server, customTitle)
	})
}
