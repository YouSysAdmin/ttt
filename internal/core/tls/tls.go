// Package tls builds *tls.Config values for the ttt server and client:
// user-provided certs, self-signed (RSA/ed25519), mutual TLS, and ACME
// (Let's Encrypt) via autocert.
package tls

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

var (
	certificateCommonName   = "Self-Signed ttt"
	certificateOrganization = []string{"ttt"}
)

type ACME struct {
	Enable   bool
	Email    string
	CacheDir string
	HTTPAddr string
	Hosts    []string
}

type ManualTLS struct {
	CertFile string
	KeyFile  string
}

type MutualTLS struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

// AutoTLS start HTTP server for get TLS cert via LetsEncrypt
func AutoTLS(ac ACME) *tls.Config {
	if !ac.Enable {
		return nil
	}
	cache := ac.CacheDir
	if cache == "" {
		cache = "certs"
	}
	addr := ac.HTTPAddr
	if addr == "" {
		addr = ":80"
	}
	m := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cache),
		Email:  ac.Email,
	}
	if len(ac.Hosts) > 0 {
		m.HostPolicy = autocert.HostWhitelist(ac.Hosts...)
	}
	go func() {
		srv := &http.Server{Addr: addr, Handler: m.HTTPHandler(nil)}
		slog.Info("ACME HTTP challenge listener", "addr", addr, "hosts", ac.Hosts)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ACME HTTP server failed", "err", err)
		}
	}()
	return &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12}
}

// LoadManualTLS load user provided TLS certs
func LoadManualTLS(m ManualTLS) (*tls.Config, error) {
	if m.CertFile == "" || m.KeyFile == "" {
		return nil, nil
	}
	pair, err := tls.LoadX509KeyPair(m.CertFile, m.KeyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}

// LoadMutualTLS builds server and client TLS configs for mutually
// authenticated connections. Both sides present the keypair and verify the
// peer against the CA bundle: the server requires and verifies client
// certificates, the client verifies the server certificate (against
// ServerName when set).
func LoadMutualTLS(m MutualTLS) (server *tls.Config, client *tls.Config, err error) {
	pair, err := tls.LoadX509KeyPair(m.CertFile, m.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load keypair: %w", err)
	}

	caPEM, err := os.ReadFile(m.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA file: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, nil, fmt.Errorf("no certificates found in CA file %q", m.CAFile)
	}

	server = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}

	client = &tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      caPool,
		ServerName:   m.ServerName,
		MinVersion:   tls.VersionTLS12,
	}

	return server, client, nil
}

// SelfSignedTLS generate self-signed TLS certs
func SelfSignedTLS(fqdn, alg string) (*tls.Config, error) {
	switch strings.ToLower(strings.TrimSpace(alg)) {
	case "ed25519":
		return selfSignedEd25519(fqdn)
	default:
		return selfSignedRSA(fqdn)
	}
}

// selfSignedRSA RSA
func selfSignedRSA(fqdn string) (*tls.Config, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: certificateCommonName, Organization: certificateOrganization},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(180 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		SignatureAlgorithm:    x509.SHA256WithRSA,
		DNSNames:              []string{fqdn, "localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(priv)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}

// selfSignedEd25519 ED25519
func selfSignedEd25519(fqdn string) (*tls.Config, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: certificateCommonName, Organization: certificateOrganization},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(180 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{fqdn, "localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tpl, tpl, priv.Public(), priv)
	if err != nil {
		return nil, err
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}
