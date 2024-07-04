# Image URL to use all building/pushing image targets
VERSION = $(shell git describe --tags --always --dirty)
REPO_ROOT := $(shell git rev-parse --show-toplevel)
ARTIFACTS ?= ${REPO_ROOT}/_artifacts
PLATFORMS ?= linux/amd64

EXPORT_RESULT?=false # for CI please set EXPORT_RESULT to true

GOTESTPKGS = $(shell go list ./... | grep -v /internal | grep -v /test)

##@ Development

## --------------------------------------
## Build
## --------------------------------------

.PHONY: build
build: ## Build the project binary
	go build -ldflags="-w -s -X 'main.version=${VERSION}'" -o=bin/nutanix-cloud-controller-manager .

## --------------------------------------
## Lint
## --------------------------------------

.PHONE: lint
lint: ## Run the linter
	golangci-lint run -v

## --------------------------------------
## Create Image
## --------------------------------------

LOCAL_IMAGE_REGISTRY ?= ko.local
IMG_NAME ?= nutanix-cloud-controller-manager
IMG_TAG ?= $(VERSION)
IMG = $(IMG_NAME):$(IMG_TAG)
IMG_REPO = $(LOCAL_IMAGE_REGISTRY)/$(IMG_NAME)

LOCALBIN ?= ${REPO_ROOT}/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: docker-build
docker-build: ## Build the image using ko
	KO_DOCKER_REPO=ko.local ko build -B --platform=${PLATFORMS} -t ${IMG_TAG} -L .

.PHONY: docker-push
docker-push: ## Build and push the image to the registry
	KO_DOCKER_REPO=$(IMG_REPO) ko build --bare --platform=${PLATFORMS} -t ${IMG_TAG} .

##@ Testing

## --------------------------------------
## Unit tests
## --------------------------------------

.PHONY: unit-test
unit-test: ## Run the unit tests of the project
	go test -v  $(GOTESTPKGS)

.PHONY: unit-test-html
unit-test-html: unit-test ## Run the unit tests of the project and export the coverage
	go tool cover -html=cover.out

.PHONY: coverage
coverage: ## Run the tests of the project and export the coverage
	go test -cover -covermode=count -coverprofile=profile.cov  $(GOTESTPKGS)
	go tool cover -func profile.cov
ifeq ($(EXPORT_RESULT), true)
	gocov convert profile.cov | gocov-xml > coverage.xml
endif

## --------------------------------------
## E2E tests
## --------------------------------------

JUNIT_REPORT_FILE ?= "junit.e2e_suite.1.xml"
GINKGO_NODES  ?= 1
E2E_DIR ?= ${REPO_ROOT}/test/e2e
E2E_CONF_FILE  ?= ${E2E_DIR}/config/nutanix.yaml
GINKGO_NOCOLOR ?= false
LABEL_FILTERS = ""
CNI_PATH_CILIUM = "${E2E_DIR}/data/cni/cilium/cilium.yaml" # helm template cilium cilium/cilium --version 1.13.0 -n kube-system --set hubble.enabled=false --set cni.chainingMode=portmap  --set sessionAffinity=true | sed 's/${BIN_PATH}/$BIN_PATH/g'

.PHONY: test-e2e
test-e2e: docker-push ## Run the e2e tests
	mkdir -p $(ARTIFACTS)
	NUTANIX_LOG_LEVEL=debug CNI=$(CNI_PATH_CILIUM) CCM_REPO=$(IMG_REPO) CCM_TAG=$(IMG_TAG) ginkgo -v \
		--trace \
		--tags=e2e \
		--label-filter=$(LABEL_FILTERS) \
		--nodes=$(GINKGO_NODES) \
		--no-color=$(GINKGO_NOCOLOR) \
		--output-dir="$(ARTIFACTS)" \
		--junit-report=${JUNIT_REPORT_FILE} \
		--timeout="1h" \
		./test/e2e -- \
		-e2e.artifacts-folder="$(ARTIFACTS)" \
		-e2e.config="$(E2E_CONF_FILE)" \

##@ Development

## --------------------------------------
## Dev
## --------------------------------------

.PHONY: nutanix-cp-endpoint-ip
nutanix-cp-endpoint-ip: ## Gets a random free IP from the control plane endpoint range set in the environment.
	@shuf --head-count=1 < <(fping -g -u "$(CONTROL_PLANE_ENDPOINT_RANGE_START)" "$(CONTROL_PLANE_ENDPOINT_RANGE_END)")

##@ Deployment

## --------------------------------------
## OpenShift specific include
## --------------------------------------

include ./openshift/openshift.mk

## --------------------------------------
## Deployment YAML manifests
## --------------------------------------

.PHONY: deployment-manifests
deployment-manifests: ## Generate the deployment manifests
	mkdir -p $(ARTIFACTS)/manifests
	cat manifests/*.yaml | envsubst > $(ARTIFACTS)/manifests/deploy_ccm.yaml

.PHONY: deploy
deploy: deployment-manifests ## Deploy the Nutanix Cloud Controller Manager to the cluster
	kubectl apply -f $(ARTIFACTS)/manifests/deploy_ccm.yaml


##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
