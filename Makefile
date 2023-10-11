# Image URL to use all building/pushing image targets
IMG ?= nutanix-cloud-controller-manager:latest
VERSION = 0.1.0
REPO_ROOT := $(shell git rev-parse --show-toplevel)
ARTIFACTS ?= ${REPO_ROOT}/_artifacts
PLATFORMS ?= linux/amd64 
IMG_TAG ?= latest

EXPORT_RESULT?=false # for CI please set EXPORT_RESULT to true

build: vendor
	GO111MODULE=on CGO_ENABLED=0 go build -ldflags="-w -s -X 'main.version=${VERSION}'" -o=bin/nutanix-cloud-controller-manager main.go

vendor:
	go mod tidy
	go mod vendor
	go mod verify

## --------------------------------------
## Unit tests
## --------------------------------------

.PHONY: unit-test
unit-test:
	go test -v ./...

.PHONY: unit-test-html
unit-test-html: unit-test
	go tool cover -html=cover.out

.PHONY: coverage
coverage: ## Run the tests of the project and export the coverage
	go test -cover -covermode=count -coverprofile=profile.cov ./...
	go tool cover -func profile.cov
ifeq ($(EXPORT_RESULT), true)
	GO111MODULE=off go get -u github.com/AlekSi/gocov-xml
	GO111MODULE=off go get -u github.com/axw/gocov/gocov
	gocov convert profile.cov | gocov-xml > coverage.xml
endif

## --------------------------------------
## OpenShift specific include
## --------------------------------------

include ./openshift/openshift.mk

## --------------------------------------
## Create Image
## --------------------------------------

LOCALBIN ?= ${REPO_ROOT}/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

KO ?= $(LOCALBIN)/ko
KO_VERSION ?= v0.11.2

.PHONY: ko
ko: $(KO) 
$(KO): $(LOCALBIN)
	test -s $(LOCALBIN)/ko || GOBIN=$(LOCALBIN) go install github.com/google/ko@$(KO_VERSION)

.PHONY: ko-build
ko-build: ko 
	KO_DOCKER_REPO=ko.local $(KO) build -B --platform=${PLATFORMS} -t ${IMG_TAG} -L .

.PHONY: docker-push
docker-push: ko-build
	docker tag ko.local/cloud-provider-nutanix:${IMG_TAG} ${IMG}
	docker push ${IMG}

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
