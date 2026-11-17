package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/caddyserver/certmagic"
	"github.com/ruiange/sing-box-panel/internal/panel"
)

func configureIPHTTPS(ctx context.Context, server *http.Server, cfg panel.Config) (func(net.Listener) error, func(), error) {
	ip, err := netip.ParseAddr(cfg.HTTPSHost)
	if err != nil {
		return nil, nil, fmt.Errorf("无法为无效 IP 配置 HTTPS: %w", err)
	}
	host := ip.Unmap().String()
	cacheDir := filepath.Join(cfg.ACMECache, "ip")
	for _, dir := range []string{cfg.ACMECache, cacheDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, nil, fmt.Errorf("创建 IP 证书缓存: %w", err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("检查 IP 证书缓存: %w", err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0700 {
			return nil, nil, fmt.Errorf("IP 证书缓存必须为权限 0700 的普通目录")
		}
	}

	manager, stopCache := newIPCertificateManager(host, cacheDir)
	manager.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(manager, certmagic.ACMEIssuer{
		CA:                   certmagic.LetsEncryptProductionCA,
		Email:                cfg.ACMEEmail,
		Agreed:               true,
		Profile:              "shortlived",
		DisableHTTPChallenge: true,
	})}
	server.TLSConfig = ipTLSConfig(manager, host)
	ctx, cancel := context.WithCancel(ctx)
	cleanup := func() {
		cancel()
		stopCache()
	}
	serve := func(listener net.Listener) error {
		// CertMagic serves TLS-ALPN validation on 443 while it obtains or renews the certificate.
		if err := manager.ManageAsync(ctx, []string{host}); err != nil {
			return fmt.Errorf("启动 IP 证书自动管理: %w", err)
		}
		return server.ServeTLS(listener, "", "")
	}
	return serve, cleanup, nil
}

func newIPCertificateManager(host, cacheDir string) (*certmagic.Config, func()) {
	var manager *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return manager, nil },
	})
	manager = certmagic.New(cache, certmagic.Config{
		DefaultServerName: host,
		Storage:           &certmagic.FileStorage{Path: cacheDir},
	})
	return manager, cache.Stop
}

func ipTLSConfig(manager *certmagic.Config, host string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Browsers omit SNI for IP addresses. CertMagic selects DefaultServerName in that case.
			if hello.ServerName != "" && hello.ServerName != host {
				return nil, fmt.Errorf("IP 面板不接受其他 TLS 主机名")
			}
			return manager.GetCertificate(hello)
		},
	}
}
