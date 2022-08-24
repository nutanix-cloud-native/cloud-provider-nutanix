## --------------------------------------
## OpenShift specific make targets
## --------------------------------------

openshift-image:
	docker build -t ${IMG} -f ./openshift/Dockerfile.openshift .

openshift-build: vendor
	GO111MODULE=on CGO_ENABLED=0 go build -ldflags="-w -s -X 'main.version=${VERSION}'" -o=bin/nutanix-cloud-controller-manager main.go