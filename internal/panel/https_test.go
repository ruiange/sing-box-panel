package panel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestNormalizeHTTPSDomain(t *testing.T) {
	for input, expected := range map[string]string{
		" Panel.Example.com ": "panel.example.com",
		"xn--fiqs8s.example":  "xn--fiqs8s.example",
	} {
		if actual, err := NormalizeHTTPSDomain(input); err != nil || actual != expected {
			t.Errorf("NormalizeHTTPSDomain(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	for _, input := range []string{
		"https://panel.example.com", "panel.example.com:443", "panel.example.com/",
		"127.0.0.1", "example.123", "localhost", "*.example.com", "-panel.example.com",
		"中国.example", "K.example", "example..com",
	} {
		if _, err := NormalizeHTTPSDomain(input); err == nil {
			t.Errorf("accepted invalid HTTPS domain %q", input)
		}
	}
}

func TestParseManualCertificate(t *testing.T) {
	const domain = "panel.example.com"
	now := time.Now()
	certPEM, keyPEM := manualTestPair(t, domain, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth)
	// Test certificate parsing even when crypto/tls doesn't populate Leaf.
	t.Setenv("GODEBUG", "x509keypairleaf=0")
	pair, err := parseManualCertificate(certPEM, keyPEM, domain)
	if err != nil || pair.Leaf == nil || !pair.Leaf.NotAfter.Equal(now.Add(time.Hour).Truncate(time.Second)) {
		t.Fatalf("valid certificate did not provide expected expiry: %v", err)
	}
}

func TestManualHTTPSRejectsUnusablePEM(t *testing.T) {
	const domain = "panel.example.com"
	now := time.Now()
	certPEM, keyPEM := manualTestPair(t, domain, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageServerAuth)
	expiredCert, expiredKey := manualTestPair(t, domain, now.Add(-2*time.Hour), now.Add(-time.Hour), x509.ExtKeyUsageServerAuth)
	futureCert, futureKey := manualTestPair(t, domain, now.Add(time.Hour), now.Add(2*time.Hour), x509.ExtKeyUsageServerAuth)
	clientCert, clientKey := manualTestPair(t, domain, now.Add(-time.Hour), now.Add(time.Hour), x509.ExtKeyUsageClientAuth)
	for _, tc := range []struct {
		cert, key     []byte
		host, message string
	}{
		{certPEM, keyPEM, "wrong.example.com", "不匹配"},
		{certPEM, expiredKey, domain, "无法加载"},
		{[]byte("invalid PEM"), keyPEM, domain, "无法加载"},
		{expiredCert, expiredKey, domain, "已过期"},
		{futureCert, futureKey, domain, "尚未生效"},
		{clientCert, clientKey, domain, "服务端认证"},
	} {
		if _, err := parseManualCertificate(tc.cert, tc.key, tc.host); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("expected %q, got %v", tc.message, err)
		}
	}
}

func manualTestPair(t *testing.T, domain string, from, to time.Time, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{domain}, NotBefore: from, NotAfter: to,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
