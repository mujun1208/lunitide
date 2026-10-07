package remotegateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"time"
)

// ensureCertificate 返回网关 TLS 证书与其 DER SHA-256 指纹。首次调用时
// 生成 ECDSA P-256 自签证书（CN=Lunitide Remote，10 年），cert.pem 与
// remote-tls-key.pem 落在数据根下并受 ACL 保护——与网关 nonce 文件同级
// 的防护（M4 加固项：私钥改 DPAPI 封存）。指纹进二维码供手机锁定证书。
func ensureCertificate(root SecureRoot) (tls.Certificate, string, error) {
	certPath, err := root.FilePath("remote-cert.pem")
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyPath, err := root.FilePath("remote-tls-key.pem")
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		fp, fpErr := certificateFingerprint(cert)
		if fpErr == nil {
			return cert, fp, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Lunitide Remote"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, "", err
	}
	for _, name := range []string{"remote-cert.pem", "remote-tls-key.pem"} {
		if err := root.ProtectRegularFile(name); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	// 立即回读，确保落盘的就是刚生成的这对。
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	fp, err := certificateFingerprint(cert)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return cert, fp, nil
}

func certificateFingerprint(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("remotegateway: empty certificate chain")
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:]), nil
}

func fmtFingerprint(fp string) string {
	if len(fp) <= 16 {
		return fp
	}
	return fp[:16]
}

// tlsConfig 构建网关 TLS 配置：仅本证书、TLS 1.2+、无客户端证书要求。
// 手机端通过二维码指纹锁定（trust-on-first-use）防中间人。
func tlsConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
}
