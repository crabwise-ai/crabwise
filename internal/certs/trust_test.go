package certs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckTrust_MissingFiles(t *testing.T) {
	res := CheckTrust("/nonexistent/ca.crt", "/nonexistent/ca.key")
	if res.Exists {
		t.Fatalf("expected Exists=false")
	}
	if res.Trusted {
		t.Fatalf("expected Trusted=false")
	}
	if res.Reason == "" {
		t.Fatalf("expected non-empty Reason")
	}
}

func TestCheckTrust_GeneratedCA_NotTrustedByDefault(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	if err := GenerateCA(certPath, keyPath); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	res := CheckTrust(certPath, keyPath)
	if !res.Exists {
		t.Fatalf("expected Exists=true, got false (%s)", res.Reason)
	}
	if res.Trusted {
		t.Fatalf("expected freshly-generated CA to be untrusted by system")
	}
	if res.Reason == "" {
		t.Fatalf("expected non-empty Reason")
	}
	if !strings.Contains(res.Reason, "not trusted") && !strings.Contains(res.Reason, "verification failed") && !strings.Contains(res.Reason, "trust store") {
		t.Fatalf("unexpected Reason: %q", res.Reason)
	}
}
