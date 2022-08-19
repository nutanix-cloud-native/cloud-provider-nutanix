# Image URL to use all building/pushing image targets
IMG ?= nutanix-ccm:latest
VERSION = 1.0.0

all: build image

build: vendor
	GO111MODULE=on CGO_ENABLED=0 go build -ldflags="-w -s -X 'main.version=${VERSION}'" -o=bin/nutanix-cloud-controller-manager main.go

image:
	docker build -t ${IMG} -f ./Dockerfile.openshift .

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
