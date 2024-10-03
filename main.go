package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/jetstack/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/jetstack/cert-manager/pkg/acme/webhook/cmd"
	"github.com/jetstack/cert-manager/pkg/issuer/acme/dns/util"

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
	client      *kubernetes.Clientset
	challengeId uuid.UUID
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

	return nil
}

func (c *nexusDnsProviderSolver) Name() string { return "nexus" }

func (c *nexusDnsProviderSolver) Present(ch *v1alpha1.ChallengeRequest) (err error) {
	recordName := extractRecordName(ch.ResolvedFQDN, ch.ResolvedZone)

	nc, err := c.nexusApiClient(ch)
	if err != nil {
		return
	}

	fmt.Printf("Presenting record for %s (%s)\n", ch.ResolvedFQDN, recordName)

	challengeId, err := challenge.CreateChallengeRecord(nc, recordName, ch.Key)
	if err != nil {
		return err
	}
	c.challengeId = challengeId
	return
}

func (c *nexusDnsProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) (err error) {
	domainName := extractDomainName(ch.ResolvedZone)

	nc, err := c.nexusApiClient(ch)
	if err != nil {
		return
	}

	fmt.Printf("Cleaning up record for %s (%s)", ch.ResolvedFQDN, domainName)

	err = challenge.DeleteChallengeRecord(nc, c.challengeId)
	return
}

func loadConfig(cfgJSON *extapi.JSON) (cfg nexusDnsProviderConfig, err error) {
	cfg = nexusDnsProviderConfig{}
	if cfgJSON == nil {
		return
	}
	err = json.Unmarshal(cfgJSON.Raw, &cfg)
	if err != nil {
		err = errors.New(fmt.Sprintf("error decoding solver config: %v", err))
		return
	}
	return
}

func (c *nexusDnsProviderSolver) nexusApiClient(ch *v1alpha1.ChallengeRequest) (client *nexus.NexusClient, err error) {
	domainName := extractDomainName(ch.ResolvedZone)
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return
	}
	keyStr, err := c.secret(cfg.ApiKeySecretRef, ch.ResourceNamespace)
	if err != nil {
		return
	}
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		err = errors.New(fmt.Sprintf("failure to decode base64 secret: %v", err))
		return
	}
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
	name := util.UnFqdn(fqdn)
	if idx := strings.Index(name, "."+util.UnFqdn(domain)); idx != -1 {
		return name[:idx]
	}
	return name
}

func extractDomainName(zone string) string {
	authZone, err := util.FindZoneByFqdn(zone, util.RecursiveNameservers)
	if err != nil {
		fmt.Printf("could not get zone by fqdn %v", err)
		return zone
	}
	return util.UnFqdn(authZone)
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
