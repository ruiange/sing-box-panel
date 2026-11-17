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
	"testing"
	"time"
)

func TestIPHTTPSUsesDefaultCertificateWithoutSNI(t *testing.T) {
	const publicIP = "203.0.113.10"
	manager, stop := newIPCertificateManager(publicIP, t.TempDir())
	defer stop()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), IPAddresses: []net.IP{net.ParseIP(publicIP)},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CacheUnmanagedTLSCertificate(context.Background(), tls.Certificate{
		Certificate: [][]byte{der}, PrivateKey: key,
	}, nil); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{TLSConfig: ipTLSConfig(manager, publicIP), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != "" {
			t.Error("IP handshake unexpectedly included SNI")
		}
		w.WriteHeader(http.StatusNoContent)
	}), ReadHeaderTimeout: time.Second}
	defer server.Close()
	go func() { _ = server.ServeTLS(listener, "", "") }()

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) {
		t.Fatal("cannot trust test IP certificate")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{ServerName: publicIP, RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("https://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected IP HTTPS response: %d", response.StatusCode)
	}
	transport.CloseIdleConnections()

	unrelated := &http.Transport{TLSClientConfig: &tls.Config{ServerName: "other.example.com", RootCAs: roots}}
	defer unrelated.CloseIdleConnections()
	if response, err := (&http.Client{Transport: unrelated, Timeout: 3 * time.Second}).Get("https://" + listener.Addr().String()); err == nil {
		response.Body.Close()
		t.Fatal("accepted a hostname outside the certificate policy")
	}
}
