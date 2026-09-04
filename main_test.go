package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
)

// TestSolverImplementsInterface ensures the solver struct satisfies the
// webhook.Solver interface. This is also a compile-time check (the
// `var _` declaration) so a missing method would prevent compilation.
func TestSolverImplementsInterface(t *testing.T) {
	var _ webhook.Solver = (*nexusDnsProviderSolver)(nil)
}

// TestRecordExtraction verifies the FQDN/zone-to-record extraction logic.
func TestRecordExtraction(t *testing.T) {
	tests := []struct {
		name   string
		fqdn   string
		zone   string
		expect string
	}{
		{"simple", "_acme-challenge.example.com.", "example.com.", "_acme-challenge"},
		{"trailing-dots", "_acme-challenge.sub.example.com.", "sub.example.com.", "_acme-challenge"},
		{"no-match-returns-full", "different.example.org.", "example.com.", "different.example.org"},
		{"no-trailing-dot", "_acme-challenge.example.com", "example.com", "_acme-challenge"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRecordName(tt.fqdn, tt.zone)
			if got != tt.expect {
				t.Errorf("extractRecordName(%q, %q) = %q; want %q", tt.fqdn, tt.zone, got, tt.expect)
			}
		})
	}
}

// TestLoadConfig verifies the JSON config unmarshalling.
func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig(&apiextv1.JSON{Raw: []byte(`{"service":"nexus.example.com","apikeysecret":{"name":"nexus-creds","key":"apikey"}}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Service != "nexus.example.com" {
		t.Errorf("Service = %q; want nexus.example.com", cfg.Service)
	}
	if cfg.ApiKeySecretRef.Name != "nexus-creds" {
		t.Errorf("ApiKeySecretRef.Name = %q; want nexus-creds", cfg.ApiKeySecretRef.Name)
	}
	if cfg.ApiKeySecretRef.Key != "apikey" {
		t.Errorf("ApiKeySecretRef.Key = %q; want apikey", cfg.ApiKeySecretRef.Key)
	}
}

// TestLoadConfigNil verifies a nil config returns the zero struct without error.
func TestLoadConfigNil(t *testing.T) {
	cfg, err := loadConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Service != "" || cfg.ApiKeySecretRef.Name != "" {
		t.Errorf("expected zero-value config, got %+v", cfg)
	}
}

// TestLoadConfigPrivateKey verifies the Ed25519 private-key secret ref is
// unmarshalled from the solver config.
func TestLoadConfigPrivateKey(t *testing.T) {
	cfg, err := loadConfig(&apiextv1.JSON{Raw: []byte(`{"service":"nexus.example.com","privatekeysecret":{"name":"nexus-creds","key":"private-key"}}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PrivateKeySecretRef.Name != "nexus-creds" {
		t.Errorf("PrivateKeySecretRef.Name = %q; want nexus-creds", cfg.PrivateKeySecretRef.Name)
	}
	if cfg.PrivateKeySecretRef.Key != "private-key" {
		t.Errorf("PrivateKeySecretRef.Key = %q; want private-key", cfg.PrivateKeySecretRef.Key)
	}
	if cfg.ApiKeySecretRef.Name != "" {
		t.Errorf("ApiKeySecretRef.Name = %q; want empty", cfg.ApiKeySecretRef.Name)
	}
}

// TestValidate covers which key combinations the solver accepts. Exactly one
// kind of key must be configured: an HMAC secret for the legacy /api/v2, or an
// Ed25519 private key for /api/v3.
func TestValidate(t *testing.T) {
	apiKey := corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "creds"},
		Key:                  "api-key",
	}
	privateKey := corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "creds"},
		Key:                  "private-key",
	}

	tests := []struct {
		name    string
		cfg     nexusDnsProviderConfig
		wantErr bool
	}{
		{"hmac only", nexusDnsProviderConfig{Service: "s", ApiKeySecretRef: apiKey}, false},
		{"private key only", nexusDnsProviderConfig{Service: "s", PrivateKeySecretRef: privateKey}, false},
		{"both keys", nexusDnsProviderConfig{Service: "s", ApiKeySecretRef: apiKey, PrivateKeySecretRef: privateKey}, true},
		{"no key", nexusDnsProviderConfig{Service: "s"}, true},
		{"no service", nexusDnsProviderConfig{ApiKeySecretRef: apiKey}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			solver := &nexusDnsProviderSolver{}
			err := solver.validate(&tt.cfg, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v; wantErr %v", err, tt.wantErr)
			}
		})
	}
}
