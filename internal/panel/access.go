package panel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/certmagic"
	"golang.org/x/crypto/acme"
)

const (
	domainListen = ":443"
	maxPEMSize   = 64 << 10
)

type accessConfig struct {
	Mode        string `json:"mode"`
	Domain      string `json:"domain,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	PrivateKey  string `json:"privateKey,omitempty"`
}

type accessStatus struct {
	Available           bool       `json:"available"`
	Mode                string     `json:"mode"`
	Domain              string     `json:"domain,omitempty"`
	HTTPSURL            string     `json:"httpsUrl,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
	ManualUploadAllowed bool       `json:"manualUploadAllowed"`
	FallbackURL         string     `json:"fallbackUrl,omitempty"`
	Warning             string     `json:"warning,omitempty"`
}

type domainSite struct {
	mode, domain string
	certificate  *tls.Certificate
	manager      *certmagic.Config
	cache        *certmagic.Cache
	ctx          context.Context
	cancel       context.CancelFunc
}

func (d *domainSite) stop() {
	if d != nil && d.cache != nil {
		d.cancel()
		d.cache.Stop()
	}
}

func (d *domainSite) expiry() (expires time.Time) {
	if d.certificate != nil {
		return d.certificate.Leaf.NotAfter
	}
	if d.cache != nil {
		for _, cert := range d.cache.AllMatchingCertificates(d.domain) {
			if cert.Leaf != nil && cert.Leaf.NotAfter.After(expires) {
				expires = cert.Leaf.NotAfter
			}
		}
	}
	return expires
}

type domainAccess struct {
	ctx        context.Context
	dir        string
	listenAddr string
	handler    http.Handler
	log        *slog.Logger

	mu       sync.Mutex // Serializes settings writes and listener changes.
	listener net.Listener
	server   *http.Server
	active   atomic.Pointer[domainSite]
	pending  atomic.Pointer[domainSite]
	warning  atomic.Pointer[string]
}

// StartDomainAccess restores the optional web-managed HTTPS listener used by direct HTTP installations.
func (s *Service) StartDomainAccess(ctx context.Context, handler http.Handler) func() {
	if !s.cfg.DirectHTTP {
		return func() {}
	}
	m := &domainAccess{ctx: ctx, dir: filepath.Join(filepath.Dir(s.cfg.DatabasePath), "panel-https"), listenAddr: domainListen, handler: handler, log: s.log}
	s.access = m
	if err := m.restore(); err != nil {
		message := "保存的 HTTPS 配置未能加载，请检查服务日志后重新配置。"
		m.warning.Store(&message)
		m.log.Error("恢复域名 HTTPS 失败", "error", err)
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.server != nil {
			_ = m.server.Close()
			_ = m.listener.Close()
			m.server = nil
		}
		m.active.Swap(nil).stop()
	}
}

func (m *domainAccess) restore() error {
	data, err := os.ReadFile(filepath.Join(m.dir, "access.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var cfg accessConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("证书设置文件格式无效")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if cfg.Mode == "off" {
		return nil
	}
	site, err := m.newSite(cfg)
	if err != nil {
		return err
	}
	if err := m.startServer(); err != nil {
		site.stop()
		return err
	}
	m.active.Store(site)
	if site.manager != nil {
		if err := site.manager.ManageAsync(site.ctx, []string{site.domain}); err != nil {
			m.active.Store(nil)
			site.stop()
			m.stopServer()
			return err
		}
	}
	return nil
}

func (c *accessConfig) validate() error {
	switch c.Mode {
	case "off":
		if c.Domain != "" || c.Certificate != "" || c.PrivateKey != "" {
			return errors.New("关闭 HTTPS 时无需填写域名或证书")
		}
	case "auto", "manual":
		host, err := NormalizeHTTPSDomain(c.Domain)
		if err != nil {
			return err
		}
		c.Domain = host
		if c.Mode == "auto" && (c.Certificate != "" || c.PrivateKey != "") {
			return errors.New("自动签发模式无需上传证书或私钥")
		}
		if c.Mode == "manual" && (len(c.Certificate) == 0 || len(c.PrivateKey) == 0 || len(c.Certificate) > maxPEMSize || len(c.PrivateKey) > maxPEMSize) {
			return errors.New("请选择 PEM 证书和私钥，每个文件最多 64 KiB")
		}
	default:
		return errors.New("请选择有效的 HTTPS 配置方式")
	}
	return nil
}

func (m *domainAccess) newSite(cfg accessConfig) (*domainSite, error) {
	site := &domainSite{mode: cfg.Mode, domain: cfg.Domain}
	if cfg.Mode == "manual" {
		cert, err := parseManualCertificate([]byte(cfg.Certificate), []byte(cfg.PrivateKey), cfg.Domain)
		if err != nil {
			return nil, err
		}
		site.certificate = &cert
		return site, nil
	}

	site.ctx, site.cancel = context.WithCancel(m.ctx)
	site.cache = certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return site.manager, nil },
	})
	site.manager = certmagic.New(site.cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: filepath.Join(m.dir, "acme")},
	})
	site.manager.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(site.manager, certmagic.ACMEIssuer{
		CA:                   certmagic.LetsEncryptProductionCA,
		Agreed:               true,
		DisableHTTPChallenge: true,
	})}
	return site, nil
}

func (m *domainAccess) startServer() error {
	if m.server != nil {
		return nil
	}
	ln, err := net.Listen("tcp", m.listenAddr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active := m.active.Load()
			if active == nil || r.TLS == nil || !strings.EqualFold(r.TLS.ServerName, active.domain) ||
				!(strings.EqualFold(r.Host, active.domain) || strings.EqualFold(r.Host, active.domain+":443")) {
				http.Error(w, "域名未配置", http.StatusMisdirectedRequest)
				return
			}
			m.handler.ServeHTTP(w, r)
		}),
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
			GetCertificate: m.getCertificate,
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      95 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	m.server = server
	m.listener = ln
	go func() {
		defer ln.Close()
		if err := server.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			m.log.Error("域名 HTTPS 监听停止", "error", err)
		}
	}()
	return nil
}

func (m *domainAccess) stopServer() {
	if m.server == nil {
		return
	}
	server := m.server
	m.server = nil
	_ = m.listener.Close()
	m.listener = nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
	}()
}

func (m *domainAccess) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	site := m.active.Load()
	// Keep the active site live during validation. A pending domain only answers the CA challenge.
	challenge := len(hello.SupportedProtos) == 1 && hello.SupportedProtos[0] == acme.ALPNProto
	if challenge {
		if candidate := m.pending.Load(); candidate != nil && strings.EqualFold(hello.ServerName, candidate.domain) {
			site = candidate
		}
	}
	if site == nil || !strings.EqualFold(hello.ServerName, site.domain) {
		return nil, errors.New("域名未配置")
	}
	if site.manager != nil {
		return site.manager.GetCertificate(hello)
	}
	return site.certificate, nil
}

func (m *domainAccess) persist(cfg accessConfig) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(m.dir, ".access-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encodeErr := json.NewEncoder(file).Encode(cfg)
	if encodeErr == nil {
		encodeErr = file.Sync()
	}
	if err := errors.Join(encodeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(m.dir, "access.json"))
}

func (s *Service) publicURL() string {
	if s.access != nil {
		if site := s.access.active.Load(); site != nil {
			return "https://" + site.domain
		}
	}
	return s.cfg.PublicURL
}

func (s *Service) accessStatus(r *http.Request) accessStatus {
	status := accessStatus{Available: s.access != nil, Mode: "off"}
	if s.access == nil {
		return status
	}
	status.FallbackURL = s.cfg.PublicURL
	status.ManualUploadAllowed = r.TLS != nil
	if warning := s.access.warning.Load(); warning != nil {
		status.Warning = *warning
	}
	if site := s.access.active.Load(); site != nil {
		status.Mode, status.Domain = site.mode, site.domain
		status.HTTPSURL = "https://" + site.domain
		if deadline := site.expiry(); !deadline.IsZero() {
			expires := deadline.UTC()
			status.ExpiresAt = &expires
		}
	}
	return status
}

func (s *Service) getAccessSettings(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, s.accessStatus(r))
}

func (s *Service) putAccessSettings(w http.ResponseWriter, r *http.Request) {
	m := s.access
	if m == nil {
		fail(w, http.StatusConflict, "当前部署方式不支持从网页管理域名证书")
		return
	}
	var cfg accessConfig
	if !readJSONLimit(w, r, &cfg, 160<<10) {
		return
	}
	if (cfg.Mode == "manual" || cfg.PrivateKey != "") && r.TLS == nil {
		fail(w, http.StatusForbidden, "请通过 HTTPS 登录后再上传证书私钥")
		return
	}
	if err := cfg.validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !m.mu.TryLock() {
		fail(w, http.StatusConflict, "正在应用另一项 HTTPS 配置，请稍后再试")
		return
	}
	defer m.mu.Unlock()

	if cfg.Mode == "off" {
		if err := m.persist(cfg); err != nil {
			s.report(w, "保存 HTTPS 设置失败", err)
			return
		}
		m.active.Swap(nil).stop()
		m.warning.Store(nil)
		respond(w, http.StatusOK, s.accessStatus(r))
		m.stopServer()
		return
	}
	if cfg.Mode == "auto" {
		if site := m.active.Load(); site != nil && site.mode == cfg.Mode && site.domain == cfg.Domain && site.expiry().After(time.Now()) {
			respond(w, http.StatusOK, s.accessStatus(r))
			return
		}
	}
	site, err := m.newSite(cfg)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			site.stop()
			if m.active.Load() == nil {
				m.stopServer()
			}
		}
	}()
	if err := m.startServer(); err != nil {
		s.log.Warn("开启域名 HTTPS 监听失败", "error", err)
		fail(w, http.StatusConflict, "无法监听 TCP 443，请检查端口占用和服务权限")
		return
	}
	if site.manager != nil {
		m.pending.Store(site)
		defer m.pending.Store(nil)
		ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
		defer cancel()
		stop := context.AfterFunc(site.ctx, cancel)
		defer stop()
		if err := site.manager.ManageSync(ctx, []string{site.domain}); err != nil {
			s.log.Warn("申请域名证书失败", "domain", site.domain, "error", err)
			fail(w, http.StatusBadGateway, "证书申请失败，请确认域名解析直连本机、公网 TCP 443 可访问，并检查服务日志")
			return
		}
	}
	if r.Context().Err() != nil {
		return
	}
	if err := m.persist(cfg); err != nil {
		s.report(w, "保存 HTTPS 设置失败", err)
		return
	}
	previous := m.active.Swap(site)
	committed = true
	m.warning.Store(nil)
	respond(w, http.StatusOK, s.accessStatus(r))
	previous.stop()
}
