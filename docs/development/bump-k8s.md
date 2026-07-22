# Bump Kubernetes Dependencies

## Steps

1. **Determine the latest patch release** for the target minor version. K8s Go module versions use the `v0.<minor>.<patch>` scheme (e.g. K8s 1.36 → `v0.36.x`). Run:
   ```
   go list -m -versions k8s.io/api | tr ' ' '\n' | grep 'v0.<minor>\.' | tail -1
   ```
   Use the latest patch version for all k8s.io modules below.

2. **Bump all k8s.io dependencies** in `go.mod` (both direct and indirect):
   ```
   go get \
     k8s.io/api@v0.<minor>.<patch> \
     k8s.io/apimachinery@v0.<minor>.<patch> \
     k8s.io/client-go@v0.<minor>.<patch> \
     k8s.io/cloud-provider@v0.<minor>.<patch> \
     k8s.io/component-base@v0.<minor>.<patch> \
     k8s.io/apiserver@v0.<minor>.<patch> \
     k8s.io/component-helpers@v0.<minor>.<patch> \
     k8s.io/controller-manager@v0.<minor>.<patch> \
     k8s.io/kms@v0.<minor>.<patch>
   ```

3. **Tidy modules**:
   ```
   go mod tidy
   ```

4. **Verify the build compiles**:
   ```
   go build ./...
   ```

5. **Run all unit tests**:
   ```
   go test ./...
   ```

6. **Update `openshift/Dockerfile.openshift`** builder and base images. To find the correct image tags, check the Go version in `go.mod` (`go` directive) and search for a matching builder image across the `github.com/openshift` org:
   ```
   gh search code "golang-<go-version>" --owner openshift --filename Dockerfile
   ```
   Pick the builder tag (e.g. `rhel-9-golang-<go-version>-openshift-<ocp-version>`) used by the majority of results and update both images in the Dockerfile accordingly. Derive the base image from the same OCP version (e.g. `<ocp-version>:base-rhel9`).

7. **Search for any remaining references** to the old K8s version across the repo (CI configs, Helm charts, documentation) and update them:
   ```
   grep -rn 'v0.<old-minor>\|1\.<old-minor>' --include='*.go' --include='*.yaml' --include='*.yml' --include='*.json' --include='*.md' --include='Dockerfile*' . | grep -v vendor/ | grep -v go.sum | grep -v go.mod
   ```
