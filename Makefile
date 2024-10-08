##
# Cert Manager Nexus Webhook
#
# @file
# @version 0.1

OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

IMAGE_NAME := "cert-manager-webhook-nexus"
# IMAGE_TAG := "latest"
IMAGE_TAG := "v0.1.3"

OUT := $(shell pwd)/_out

KUBEBUILDER_VERSION=4.2.0

$(shell mkdir -p "$(OUT)")

test: _test/kubebuilder
	go test -v .

_test/kubebuilder:
	curl -fsSL https://github.com/kubernetes-sigs/kubebuilder/releases/download/v$(KUBEBUILDER_VERSION)/kubebuilder_$(OS)_$(ARCH) -o kubebuilder-tools.tar.gz
	mkdir -p _test/kubebuilder
	tar -xvf kubebuilder-tools.tar.gz
	mv kubebuilder_$(KUBEBUILDER_VERSION)_$(OS)_$(ARCH)/bin _test/kubebuilder/
	rm kubebuilder-tools.tar.gz
	rm -R kubebuilder_$(KUBEBUILDER_VERSION)_$(OS)_$(ARCH)

clean: clean-kubebuilder

clean-kubebuilder:
	rm -Rf _test/kubebuilder

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
