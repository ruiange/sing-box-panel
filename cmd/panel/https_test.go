package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/panel"
	"golang.org/x/crypto/acme/autocert"
)

func TestStandaloneHTTPSUsesCachedCertificate(t *testing.T) {
	const domain = "panel.example.com"
	cfg := panel.Config{HTTPSHost: domain, ACMECache: filepath.Join(t.TempDir(), "certs")}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			t.Error("request reached the server without TLS")
		}
		w.WriteHeader(http.StatusNoContent)
	}), ReadHeaderTimeout: time.Second}
	serve, err := configureHTTPS(server, cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{domain},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	entry := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}), certificatePEM...)
	if err := (autocert.DirCache(cfg.ACMECache)).Put(context.Background(), domain, entry); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer server.Close()
	go func() { _ = serve(listener) }()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("cannot trust test certificate")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{ServerName: domain, RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("https://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected HTTPS response: %d", response.StatusCode)
	}
	transport.CloseIdleConnections()
	transport = &http.Transport{TLSClientConfig: &tls.Config{ServerName: "other.example.com", RootCAs: roots}}
	defer transport.CloseIdleConnections()
	if response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get("https://" + listener.Addr().String()); err == nil {
		response.Body.Close()
		t.Fatal("accepted a hostname outside the certificate policy")
	}
}
