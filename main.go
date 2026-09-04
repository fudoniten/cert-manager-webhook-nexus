package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"

	"github.com/fudoniten/nexus-go/nexus"
	"github.com/fudoniten/nexus-go/nexus/challenge"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("Missing required env variable GROUP_NAME")
	}

	cmd.RunWebhookServer(GroupName, &nexusDnsProviderSolver{})
}

type nexusDnsProviderSolver struct {
	client *kubernetes.Clientset

	// A single solver instance serves every challenge concurrently, so the
	// per-challenge record IDs returned by Present must be tracked in a map
	// keyed by challenge identity rather than a single shared field.
	mu           sync.Mutex
	challengeIds map[string]uuid.UUID
}

// challengeKey uniquely identifies a challenge so that Present and CleanUp for
// the same ChallengeRequest agree on which record ID to use.
func challengeKey(ch *v1alpha1.ChallengeRequest) string {
	return ch.ResolvedFQDN + "|" + ch.Key
}

type nexusDnsProviderConfig struct {
	Service string `json:"service"`

	// ApiKeySecretRef names a secret holding the shared HMAC key this
	// service authenticates with against the legacy /api/v2.
	ApiKeySecretRef corev1.SecretKeySelector `json:"apikeysecret"`

	// PrivateKeySecretRef names a secret holding this service's Ed25519
	// private key, as written by nexus-generate-key --keypair, for the
	// public-key /api/v3. Only the client holds a private key; the matching
	// public key sits on the Nexus server in plaintext, so migrating to one
	// removes the shared secret the server previously had to keep.
	//
	// Exactly one of ApiKeySecretRef and PrivateKeySecretRef is set.
	PrivateKeySecretRef corev1.SecretKeySelector `json:"privatekeysecret"`
}

func (c *nexusDnsProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.client = cl
	c.challengeIds = make(map[string]uuid.UUID)

	return nil
}

func (c *nexusDnsProviderSolver) Name() string { return "nexus" }

func (c *nexusDnsProviderSolver) Present(ch *v1alpha1.ChallengeRequest) (err error) {
	recordName := extractRecordName(ch.ResolvedFQDN, ch.ResolvedZone)

	nc, err := c.nexusApiClient(ch)
	if err != nil {
		return
	}

	log.Printf("Presenting record for %s (%s)\n", ch.ResolvedFQDN, recordName)

	challengeId, err := challenge.CreateChallengeRecord(nc, recordName, ch.Key)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.challengeIds[challengeKey(ch)] = challengeId
	c.mu.Unlock()
	return
}

func (c *nexusDnsProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) (err error) {
	domainName := extractRecordName(ch.ResolvedFQDN, ch.ResolvedZone)

	nc, err := c.nexusApiClient(ch)
	if err != nil {
		return
	}

	c.mu.Lock()
	challengeId, ok := c.challengeIds[challengeKey(ch)]
	delete(c.challengeIds, challengeKey(ch))
	c.mu.Unlock()
	if !ok {
		// No record was presented for this challenge (or it was already
		// cleaned up), so there is nothing to delete.
		log.Printf("No challenge record to clean up for %s (%s)\n", ch.ResolvedFQDN, domainName)
		return nil
	}

	log.Printf("Cleaning up record for %s (%s)\n", ch.ResolvedFQDN, domainName)

	err = challenge.DeleteChallengeRecord(nc, challengeId)
	return
}

func loadConfig(cfgJSON *apiextv1.JSON) (cfg nexusDnsProviderConfig, err error) {
	cfg = nexusDnsProviderConfig{}
	if cfgJSON == nil {
		return
	}
	err = json.Unmarshal(cfgJSON.Raw, &cfg)
	if err != nil {
		err = fmt.Errorf("error decoding solver config: %v", err)
		return
	}
	return
}

func (c *nexusDnsProviderSolver) nexusApiClient(ch *v1alpha1.ChallengeRequest) (client *nexus.NexusClient, err error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return
	}
	if err = c.validate(&cfg, ch.AllowAmbientCredentials); err != nil {
		return
	}
	signer, err := c.signer(&cfg, ch.ResourceNamespace)
	if err != nil {
		return
	}
	// ResolvedZone is the apex zone — no need to look it up.
	domainName := strings.TrimSuffix(ch.ResolvedZone, ".")
	client, err = nexus.NewWithSigner(domainName, cfg.Service, signer)
	if err != nil {
		return
	}
	return
}

// signer loads the configured key and builds a signer for it. Which config
// field names the secret decides which kind of key is accepted, and so which
// API version requests go to; pointing a field at the wrong kind of key fails
// here, naming the algorithm found, rather than as an unexplained 401 from the
// Nexus server.
func (c *nexusDnsProviderSolver) signer(cfg *nexusDnsProviderConfig, namespace string) (nexus.Signer, error) {
	if cfg.PrivateKeySecretRef.Name != "" {
		keyStr, err := c.secret(cfg.PrivateKeySecretRef, namespace)
		if err != nil {
			return nil, err
		}
		signer, err := nexus.ParseEd25519Key(keyStr)
		if err != nil {
			return nil, fmt.Errorf("failure to load private key from privatekeysecret: %v", err)
		}
		return signer, nil
	}

	keyStr, err := c.secret(cfg.ApiKeySecretRef, namespace)
	if err != nil {
		return nil, err
	}
	signer, err := nexus.ParseHMACKey(keyStr)
	if err != nil {
		return nil, fmt.Errorf("failure to load key from apikeysecret: %v", err)
	}
	return signer, nil
}

func (c *nexusDnsProviderSolver) validate(cfg *nexusDnsProviderConfig, allowAmbientCredentials bool) error {
	if allowAmbientCredentials {
		return nil
	}
	if cfg.Service == "" {
		return errors.New("No service name provided in config")
	}
	hasApiKey := cfg.ApiKeySecretRef.Name != ""
	hasPrivateKey := cfg.PrivateKeySecretRef.Name != ""
	if hasApiKey && hasPrivateKey {
		return errors.New("Both apikeysecret and privatekeysecret provided in config; set only one")
	}
	if !hasApiKey && !hasPrivateKey {
		return errors.New("No nexus service key provided in config: set privatekeysecret (Ed25519, /api/v3) or apikeysecret (legacy HMAC, /api/v2)")
	}
	return nil
}

func extractRecordName(fqdn, domain string) string {
	name := strings.TrimSuffix(fqdn, ".")
	zone := strings.TrimSuffix(domain, ".")
	if idx := strings.Index(name, "."+zone); idx != -1 {
		return name[:idx]
	}
	return name
}

func (c *nexusDnsProviderSolver) secret(ref corev1.SecretKeySelector, namespace string) (key string, err error) {
	if ref.Name == "" {
		err = errors.New("secret name not provided")
		return
	}

	keyValue, err := c.client.CoreV1().Secrets(namespace).Get(context.Background(), ref.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	key = string(keyValue.Data[ref.Key])
	return
}
