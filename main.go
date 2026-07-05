package main

import (
	"context"
	"encoding/base64"
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
	Service         string                   `json:"service"`
	ApiKeySecretRef corev1.SecretKeySelector `json:"apikeysecret"`
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
	keyStr, err := c.secret(cfg.ApiKeySecretRef, ch.ResourceNamespace)
	if err != nil {
		return
	}
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		err = fmt.Errorf("failure to decode base64 secret: %v", err)
		return
	}
	// ResolvedZone is the apex zone — no need to look it up.
	domainName := strings.TrimSuffix(ch.ResolvedZone, ".")
	client, err = nexus.New(domainName, cfg.Service, key)
	if err != nil {
		return
	}
	return
}

func (c *nexusDnsProviderSolver) validate(cfg *nexusDnsProviderConfig, allowAmbientCredentials bool) error {
	if allowAmbientCredentials {
		return nil
	}
	if cfg.Service == "" {
		return errors.New("No service name provided in config")
	}
	if cfg.ApiKeySecretRef.Name == "" {
		return errors.New("No nexus service key provided in config")
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
