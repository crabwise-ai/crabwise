package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type TrustCheckResult struct {
	CertPath string
	KeyPath  string
	Exists   bool
	Trusted  bool
	Reason   string
}

func CheckTrust(certPath, keyPath string) TrustCheckResult {
	res := TrustCheckResult{
		CertPath: certPath,
		KeyPath:  keyPath,
	}

	if _, err := os.Stat(certPath); err != nil {
		res.Reason = fmt.Sprintf("CA certificate not found: %v", err)
		return res
	}
	if _, err := os.Stat(keyPath); err != nil {
		res.Reason = fmt.Sprintf("CA key not found: %v", err)
		return res
	}
	res.Exists = true

	ca, err := LoadCA(certPath, keyPath)
	if err != nil {
		res.Reason = err.Error()
		return res
	}

	leafX509, err := mintLeafCert(ca, "crabwise.local")
	if err != nil {
		res.Reason = err.Error()
		return res
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		res.Reason = fmt.Sprintf("load system trust store: %v", err)
		return res
	}
	if roots == nil {
		res.Reason = "load system trust store: returned nil pool"
		return res
	}

	_, err = leafX509.Verify(x509.VerifyOptions{
		DNSName: "crabwise.local",
		Roots:   roots,
	})
	if err == nil {
		res.Trusted = true
		res.Reason = "trusted by system"
		return res
	}

	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		res.Reason = "not trusted by system trust store"
		return res
	}
	res.Reason = fmt.Sprintf("certificate verification failed: %v", err)
	return res
}

func mintLeafCert(ca *tls.Certificate, hostname string) (*x509.Certificate, error) {
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

	caKey, ok := ca.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("CA private key is not ECDSA")
	}
	if ca.Leaf == nil {
		return nil, fmt.Errorf("CA certificate leaf is nil")
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Leaf, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("signing certificate for %s: %w", hostname, err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse generated leaf certificate: %w", err)
	}
	return leaf, nil
}

type TrustCommands struct {
	OS               string
	SystemTrustCmd   string
	NodeExtraCACerts string
}

func CommandsForOS(certPath string) TrustCommands {
	goos := runtime.GOOS
	out := TrustCommands{
		OS:               goos,
		NodeExtraCACerts: nodeExtraCACertsCmd(goos, certPath),
	}

	switch goos {
	case "darwin":
		out.SystemTrustCmd = fmt.Sprintf(
			"sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain %s",
			shellQuotePosix(certPath),
		)
	case "linux":
		out.SystemTrustCmd = linuxTrustCmd(certPath)
	case "windows":
		out.SystemTrustCmd = fmt.Sprintf(
			"certutil -addstore -f ROOT %s",
			shellQuoteWindows(certPath),
		)
	default:
		out.SystemTrustCmd = ""
	}

	return out
}

func HasClipboard() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("pbcopy")
		return err == nil
	case "windows":
		_, err := exec.LookPath("clip")
		return err == nil
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return true
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			return true
		}
		if _, err := exec.LookPath("xsel"); err == nil {
			return true
		}
		return false
	}
}

func CopyToClipboard(text string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return copyPipe("pbcopy", nil, text)
	case "windows":
		return copyPipe("clip", nil, text)
	default:
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return copyPipe("wl-copy", nil, text)
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			return copyPipe("xclip", []string{"-selection", "clipboard"}, text)
		}
		if _, err := exec.LookPath("xsel"); err == nil {
			return copyPipe("xsel", []string{"--clipboard", "--input"}, text)
		}
		return "", fmt.Errorf("no clipboard helper found (install wl-clipboard or xclip)")
	}
}

func copyPipe(bin string, args []string, text string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return bin, nil
}

func linuxTrustCmd(certPath string) string {
	quoted := shellQuotePosix(certPath)

	// Arch Linux (p11-kit)
	if _, err := exec.LookPath("trust"); err == nil {
		return fmt.Sprintf("sudo trust anchor --store %s", quoted)
	}

	// Fedora / RHEL / CentOS
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return fmt.Sprintf("sudo cp %s /etc/pki/ca-trust/source/anchors/crabwise.crt && sudo update-ca-trust", quoted)
	}

	// Debian / Ubuntu / Alpine
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		return fmt.Sprintf("sudo cp %s /usr/local/share/ca-certificates/crabwise.crt && sudo update-ca-certificates", quoted)
	}

	return ""
}

func nodeExtraCACertsCmd(goos, certPath string) string {
	switch goos {
	case "windows":
		// Works in cmd.exe and PowerShell.
		return fmt.Sprintf("set NODE_EXTRA_CA_CERTS=%s", shellQuoteWindows(certPath))
	default:
		return fmt.Sprintf("export NODE_EXTRA_CA_CERTS=%s", shellQuotePosix(certPath))
	}
}

func shellQuotePosix(s string) string {
	// Safe single-quote for sh/bash/zsh.
	// foo'bar -> 'foo'"'"'bar'
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += "'\"'\"'"
			continue
		}
		out += string(r)
	}
	out += "'"
	return out
}

func shellQuoteWindows(s string) string {
	// Basic quoting for cmd.exe / PowerShell path args.
	// Double quotes are doubled for cmd.exe compatibility.
	escaped := ""
	for _, r := range s {
		if r == '"' {
			escaped += `""`
			continue
		}
		escaped += string(r)
	}
	return `"` + escaped + `"`
}
