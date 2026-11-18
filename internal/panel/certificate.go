package panel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"slices"
	"time"
)

func parseManualCertificate(certPEM, keyPEM []byte, host string) (tls.Certificate, error) {
	host, err := NormalizeHTTPSDomain(host)
	if err != nil {
		return tls.Certificate{}, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("无法加载 PEM 证书和私钥: %w", err)
	}
	if pair.Leaf == nil {
		// GODEBUG=x509keypairleaf=0 disables the pre-parsed leaf.
		pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("无法解析站点证书: %w", err)
		}
	}
	if err := pair.Leaf.VerifyHostname(host); err != nil {
		return tls.Certificate{}, fmt.Errorf("证书与域名 %s 不匹配", host)
	}
	now := time.Now()
	if now.Before(pair.Leaf.NotBefore) || !now.Before(pair.Leaf.NotAfter) {
		return tls.Certificate{}, fmt.Errorf("站点证书已过期或尚未生效")
	}
	if len(pair.Leaf.ExtKeyUsage)+len(pair.Leaf.UnknownExtKeyUsage) > 0 &&
		!slices.Contains(pair.Leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) &&
		!slices.Contains(pair.Leaf.ExtKeyUsage, x509.ExtKeyUsageAny) {
		return tls.Certificate{}, fmt.Errorf("站点证书不适用于 TLS 服务端认证")
	}
	return pair, nil
}
