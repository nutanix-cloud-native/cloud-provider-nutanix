# Image URL to use all building/pushing image targets
IMG ?= nutanix-cloud-controller-manager:latest
VERSION = 0.1.0

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
	go test --cover -v ./... -coverprofile cover.out

.PHONY: unit-test-html
unit-test-html: unit-test
	go tool cover -html=cover.out

## --------------------------------------
## OpenShift specific include
## --------------------------------------

include ./openshift/openshift.mk
