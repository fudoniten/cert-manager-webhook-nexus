##
# Cert Manager Nexus Webhook
#
# @file
# @version 1.0.0

OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

IMAGE_NAME := "cert-manager-webhook-nexus"
IMAGE_TAG := "v1.0.0"

OUT := $(shell pwd)/_out

# Run unit tests. The cert-manager test/acme conformance suite requires
# envtest (kube-apiserver/etcd binaries downloaded at test time) plus a live
# DNS server, so it is opt-in. Use `make test-conformance` to run it; you must
# have KUBEBUILDER_ASSETS set (the envtest binaries).
test:
	go test -v -short ./...

# Run the full ACME conformance suite. Requires envtest binaries at
# KUBEBUILDER_ASSETS and a DNS server reachable at TEST_DNS_SERVER.
test-conformance:
	TEST_DNS_SERVER?=127.0.0.1:59351 KUBEBUILDER_ASSETS?=$$(setup-envtest use 1.32.x -p path) \
	  go test -v -run TestRunsSuite ./...

build:
	docker build -t "$(IMAGE_NAME):$(IMAGE_TAG)" .

.PHONY: rendered-manifest.yaml
rendered-manifest.yaml:
	helm template \
	    --name cert-manager-webhook-nexus \
        --set image.repository=$(IMAGE_NAME) \
        --set image.tag=$(IMAGE_TAG) \
        deploy/cert-manager-webhook-nexus > "$(OUT)/rendered-manifest.yaml"

# end
