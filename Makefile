# Image URL to use all building/pushing image targets
VERSION = $(shell git describe --tags --always --dirty)
REPO_ROOT := $(shell git rev-parse --show-toplevel)
ARTIFACTS ?= ${REPO_ROOT}/_artifacts
PLATFORMS ?= linux/amd64 

EXPORT_RESULT?=false # for CI please set EXPORT_RESULT to true

GOTESTPKGS = $(shell go list ./... | grep -v /internal | grep -v /test)

.PHONY: build
build:
	go build -ldflags="-w -s -X 'main.version=${VERSION}'" -o=bin/nutanix-cloud-controller-manager .

## --------------------------------------
## Unit tests
## --------------------------------------

.PHONY: unit-test
unit-test:
	go test -v  $(GOTESTPKGS)

.PHONY: unit-test-html
unit-test-html: unit-test
	go tool cover -html=cover.out

.PHONY: coverage
coverage: ## Run the tests of the project and export the coverage
	go test -cover -covermode=count -coverprofile=profile.cov  $(GOTESTPKGS)
	go tool cover -func profile.cov
ifeq ($(EXPORT_RESULT), true)
	gocov convert profile.cov | gocov-xml > coverage.xml
endif

## --------------------------------------
## Create Image
## --------------------------------------
LOCAL_IMAGE_REGISTRY ?= ko.local
IMG_NAME ?= nutanix-cloud-controller-manager
IMG_TAG ?= $(VERSION)
IMG = $(IMG_NAME):$(IMG_TAG)

LOCALBIN ?= ${REPO_ROOT}/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: ko-build
ko-build:
	KO_DOCKER_REPO=$(LOCAL_IMAGE_REGISTRY) ko build -B --platform=${PLATFORMS} -t ${IMG_TAG} -L .

.PHONY: docker-push
docker-push:
	KO_DOCKER_REPO=$(LOCAL_IMAGE_REGISTRY) ko build --bare --platform=${PLATFORMS} -t ${IMG_TAG} .

## --------------------------------------
## OpenShift specific include
## --------------------------------------

include ./openshift/openshift.mk

## --------------------------------------
## Deployment YAML manifests
## --------------------------------------

.PHONY: deployment-manifests
deployment-manifests:
	mkdir -p $(ARTIFACTS)/manifests
	cat manifests/*.yaml | envsubst > $(ARTIFACTS)/manifests/deploy_ccm.yaml

.PHONY: deploy
deploy: deployment-manifests
	kubectl apply -f $(ARTIFACTS)/manifests/deploy_ccm.yaml

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
test-e2e: docker-push
	mkdir -p $(ARTIFACTS)
	NUTANIX_LOG_LEVEL=debug CNI=$(CNI_PATH_CILIUM) CCM_REPO=$(LOCAL_IMAGE_REGISTRY) CCM_TAG=$(IMG_TAG) ginkgo -v \
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


