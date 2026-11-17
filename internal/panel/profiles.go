package panel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
)

func (cfg *Config) configureProfiles(realityKey string) error {
	enabled, err := strconv.ParseBool(env("PANEL_NODE_MULTIPROTOCOL", "false"))
	if err != nil {
		return fmt.Errorf("PANEL_NODE_MULTIPROTOCOL 必须是 true 或 false")
	}
	if !enabled {
		return nil
	}
	base := cfg.Node.ListenPort
	if base < 1024 || base > 65535-12 {
		return fmt.Errorf("多协议模式的起始端口必须介于 1024 和 65523")
	}
	_, webPort, _ := net.SplitHostPort(cfg.Listen)
	_, statsPort, _ := net.SplitHostPort(cfg.Node.StatsAddress)
	for _, port := range []string{webPort, statsPort} {
		if number, err := strconv.Atoi(port); err == nil && number >= base && number <= base+12 {
			return fmt.Errorf("多协议节点端口 %d 至 %d 与面板或统计端口冲突", base, base+12)
		}
	}
	certificateName := cfg.Endpoint.Host
	if net.ParseIP(certificateName) != nil {
		certificateName = "panel-node.invalid"
	}
	certificateName = env("PANEL_NODE_TLS_NAME", certificateName)
	if !validDNSName(certificateName) || net.ParseIP(certificateName) != nil {
		return fmt.Errorf("PANEL_NODE_TLS_NAME 必须是有效的 TLS 名称")
	}
	certDir, err := filepath.Abs(filepath.Join(filepath.Dir(cfg.DatabasePath), "node-tls"))
	if err != nil {
		return err
	}
	profiles := node.NewProfiles(base)
	profiles.TLSName = certificateName
	profiles.ShadowTLSServerName = env("PANEL_SHADOWTLS_SERVER_NAME", "www.microsoft.com")
	if !validDNSName(profiles.ShadowTLSServerName) || net.ParseIP(profiles.ShadowTLSServerName) != nil {
		return fmt.Errorf("PANEL_SHADOWTLS_SERVER_NAME 必须是有效的域名")
	}
	profiles.TLSCertificatePath = filepath.Join(certDir, "certificate.pem")
	profiles.TLSKeyPath = filepath.Join(certDir, "private-key.pem")
	profiles.TLSCertificatePEM, profiles.TLSPin, err = nodeCertificate(certDir, profiles.TLSCertificatePath, profiles.TLSKeyPath, certificateName, cfg.Endpoint.Host)
	if err != nil {
		return fmt.Errorf("初始化节点 TLS 证书: %w", err)
	}
	profiles.SSBaseKey = sharedSSKey(realityKey, "shadowsocks")
	profiles.ShadowSSBaseKey = sharedSSKey(realityKey, "shadowtls-shadowsocks")
	cfg.Node.Profiles = &profiles
	cfg.Endpoint.Profiles = &profiles
	return nil
}

func sharedSSKey(secret, label string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("sing-box-panel/ss-key/v1/" + label))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)[:16])
}

func nodeCertificate(dir, certPath, keyPath, name, host string) (string, string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", "", fmt.Errorf("节点证书目录须为权限 0700 的目录")
	}
	key, err := readTLSFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, certErr := os.Lstat(certPath); certErr == nil || !errors.Is(certErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("证书存在但缺少对应的私钥")
		}
		priv, keyErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if keyErr != nil {
			return "", "", keyErr
		}
		der, keyErr := x509.MarshalECPrivateKey(priv)
		if keyErr != nil {
			return "", "", keyErr
		}
		key = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		err = writePrivateFile(keyPath, key)
	}
	if err != nil {
		return "", "", err
	}
	certPEM, err := readTLSFile(certPath)
	if errors.Is(err, os.ErrNotExist) {
		certPEM, err = issueNodeCertificate(key, name, host)
		if err == nil {
			err = writePrivateFile(certPath, certPEM)
		}
	}
	if err != nil {
		return "", "", err
	}
	pair, err := tls.X509KeyPair(certPEM, key)
	if err != nil {
		return "", "", err
	}
	if err := pair.Leaf.VerifyHostname(name); err != nil {
		return "", "", err
	}
	if err := pair.Leaf.VerifyHostname(host); err != nil {
		return "", "", err
	}
	if time.Now().After(pair.Leaf.NotAfter) || time.Now().Before(pair.Leaf.NotBefore) {
		return "", "", fmt.Errorf("节点证书已失效")
	}
	digest := sha256.Sum256(pair.Leaf.RawSubjectPublicKeyInfo)
	return string(certPEM), base64.StdEncoding.EncodeToString(digest[:]), nil
}

func readTLSFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, fmt.Errorf("节点证书和私钥须为权限 0600 的普通文件")
	}
	return os.ReadFile(path)
}

func issueNodeCertificate(key []byte, name, host string) ([]byte, error) {
	block, rest := pem.Decode(key)
	if block == nil || len(rest) != 0 || block.Type != "EC PRIVATE KEY" {
		return nil, fmt.Errorf("节点私钥格式无效")
	}
	priv, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	serial.Add(serial, big.NewInt(1))
	now := time.Now()
	cert := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(0, 0, 180),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = []net.IP{ip}
	} else if host != name {
		cert.DNSNames = append(cert.DNSNames, host)
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func writePrivateFile(path string, payload []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".node-tls-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}
