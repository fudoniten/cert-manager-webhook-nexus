package main

import (
	"testing"

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
