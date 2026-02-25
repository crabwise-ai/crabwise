package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func GenerateCA(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating CA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Crabwise Local CA",
			Organization: []string{"Crabwise"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating CA certificate: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0700); err != nil {
		return fmt.Errorf("creating cert directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return fmt.Errorf("creating key directory: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("writing CA certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshalling CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("writing CA key: %w", err)
	}

	return nil
}

func LoadCA(certPath, keyPath string) (*tls.Certificate, error) {
	info, err := os.Stat(keyPath)
	if err != nil {
		return nil, fmt.Errorf("stat CA key file: %w", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		return nil, fmt.Errorf(
			"CA key file %s has permissions %04o; expected 0600 — run: chmod 600 %s",
			keyPath, perm, keyPath,
		)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA key: %w", err)
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing CA key pair: %w", err)
	}

	// Parse the leaf so pair.Leaf is populated for signing operations.
	pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	return &pair, nil
}

type CertCache struct {
	ca      *tls.Certificate
	mu      sync.RWMutex
	certs   map[string]*tls.Certificate
	maxSize int
}

func NewCertCache(ca *tls.Certificate, maxSize int) *CertCache {
	return &CertCache{
		ca:      ca,
		certs:   make(map[string]*tls.Certificate),
		maxSize: maxSize,
	}
}

func (c *CertCache) GetOrCreate(hostname string) (*tls.Certificate, error) {
	c.mu.RLock()
	if cert, ok := c.certs[hostname]; ok {
		c.mu.RUnlock()
		return cert, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check after acquiring write lock.
	if cert, ok := c.certs[hostname]; ok {
		return cert, nil
	}

	if len(c.certs) >= c.maxSize {
		c.certs = make(map[string]*tls.Certificate)
	}

	cert, err := c.generateCert(hostname)
	if err != nil {
		return nil, err
	}
	c.certs[hostname] = cert
	return cert, nil
}

func (c *CertCache) generateCert(hostname string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key for %s: %w", hostname, err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: hostname,
		},
		DNSNames:  []string{hostname},
		NotBefore: now,
		NotAfter:  now.Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	caKey, ok := c.ca.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("CA private key is not ECDSA")
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, c.ca.Leaf, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("signing certificate for %s: %w", hostname, err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}
