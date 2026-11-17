package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/ruiange/sing-box-panel/internal/panel"
	"golang.org/x/crypto/acme/autocert"
)

func configureHTTPS(server *http.Server, cfg panel.Config) (func(net.Listener) error, error) {
	if cfg.HTTPSHost == "" {
		return server.Serve, nil
	}
	if err := os.MkdirAll(cfg.ACMECache, 0700); err != nil {
		return nil, fmt.Errorf("创建证书缓存: %w", err)
	}
	info, err := os.Lstat(cfg.ACMECache)
	if err != nil {
		return nil, fmt.Errorf("检查证书缓存: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("证书缓存必须为权限 0700 的普通目录")
	}
	manager := &autocert.Manager{
		Cache:      autocert.DirCache(cfg.ACMECache),
		Prompt:     autocert.AcceptTOS,
		Email:      cfg.ACMEEmail,
		HostPolicy: autocert.HostWhitelist(cfg.HTTPSHost),
	}
	// TLS-ALPN-01 validation uses the same public 443 listener as the panel.
	server.TLSConfig = manager.TLSConfig()
	server.TLSConfig.MinVersion = tls.VersionTLS12
	return func(listener net.Listener) error {
		return server.ServeTLS(listener, "", "")
	}, nil
}
