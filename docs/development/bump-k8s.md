# Bump Kubernetes Dependencies

## Steps

1. **Determine the target patch release**. Honor an explicitly requested patch; otherwise choose the latest stable patch for the target minor. K8s Go module versions use the `v0.<minor>.<patch>` scheme (e.g. K8s 1.37 → `v0.37.x`). Run:
   ```
   go list -m -versions k8s.io/api | tr ' ' '\n' | grep -E '^v0\.<minor>\.[0-9]+$' | sort -V | tail -1
   ```
   Use the selected patch version for all Kubernetes release modules below.

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

   **Update the separate E2E module** in `test/e2e/go.mod` as well. From `test/e2e`, bump its Kubernetes release modules (both direct and indirect) to the same version and tidy its own `go.sum`:
   ```
   cd test/e2e
   go get \
     k8s.io/api@v0.<minor>.<patch> \
     k8s.io/apimachinery@v0.<minor>.<patch> \
     k8s.io/apiextensions-apiserver@v0.<minor>.<patch> \
     k8s.io/apiserver@v0.<minor>.<patch> \
     k8s.io/client-go@v0.<minor>.<patch> \
     k8s.io/cluster-bootstrap@v0.<minor>.<patch> \
     k8s.io/component-base@v0.<minor>.<patch>
   go mod tidy
   cd ../..
   ```
   Inspect both modules for any additional Kubernetes release modules that need alignment. Do not assign Kubernetes release versions to independently versioned modules such as `k8s.io/klog/v2`, `k8s.io/utils`, or `k8s.io/kube-openapi`.
   If the E2E compile check exposes API incompatibilities, align `sigs.k8s.io/controller-runtime` and CAPI libraries with the target Kubernetes release rather than leaving an unbuildable module.
   CAPI 1.14+ also publishes a separate `sigs.k8s.io/cluster-api/api` module; pin it to the same release as `cluster-api` and `cluster-api/test` (dependency-local `replace` directives do not apply here).

4. **Verify the build compiles**:
   ```
   go build ./...
   (cd test/e2e && go test -tags=e2e ./... -run '^$')
   ```
   The second command compiles E2E tests without provisioning clusters.

5. **Run all unit tests**:
   ```
   go test ./...
   devbox run -- make lint
   ```
   Use the repo-defined linter, not a global binary. If Devbox is unavailable, run `nix shell ./hack/flakes#golangci-lint -c make lint`.

6. **Update `openshift/Dockerfile.openshift`** builder and base images. To find the correct image tags, check the Go version in `go.mod` (`go` directive) and search for a matching builder image across the `github.com/openshift` org:
   ```
   gh search code "golang-<go-version>" --owner openshift --filename Dockerfile
   ```
   Pick the builder tag (e.g. `rhel-9-golang-<go-version>-openshift-<ocp-version>`) used by the majority of results and update both images in the Dockerfile accordingly. Derive the base image from the same OCP version (e.g. `<ocp-version>:base-rhel9`).

7. **Update E2E cluster defaults and images** in `test/e2e/config/nutanix.yaml`:
   - Evaluate `KUBERNETES_VERSION_MANAGEMENT` and `KUBERNETES_VERSION` separately. The management version requires a published `kindest/node` image; verify available tags rather than assuming only `.0` releases exist.
   - Update `NUTANIX_MACHINE_TEMPLATE_IMAGE_NAME`, `NUTANIX_NON_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME`, and `NUTANIX_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME` only to images confirmed to exist, contain the selected Kubernetes version, and be visible to the relevant PC projects. Do not invent image names by substituting a version.
   - Image names in the repo are defaults, not an inventory of available Prism Central images. When checking images through Prism Central APIs, use v4 only; never use v3, including for ad-hoc lookups. Do not print credentials. Until a matching newer image is confirmed, retain the existing image and its matching workload version together, and report the deferred workload bump.
   - Verify the CAPI core/bootstrap/control-plane and CAPX provider releases in the config support the selected management/workload versions. The CAPI Go module version is not the deployed provider version. In particular, CAPI v1.11.2 injects `ControlPlaneKubeletLocalMode`, which kubeadm no longer accepts starting with Kubernetes 1.36. Resolve provider compatibility before raising workload defaults; record blockers or justified version exceptions explicitly.
   - When changing deployed providers, update every preload image, manifest URL, and version entry together, plus the corresponding release series in `test/e2e/data/shared/metadata.yaml`. Update any now-obsolete compatibility warnings in `.agents/skills/run-e2e-tests/SKILL.md`.
   - Run the relevant E2E scenarios when a suitable environment is available. A compile-only check does not validate cluster provisioning.

8. **Update the local kubectl package**. `devbox.json` selects `kubectl@latest`, but `devbox.lock` pins the resolved release. Refresh only that package through Devbox:
   ```
   devbox update kubectl
   devbox run -- kubectl version --client
   ```
   Verify the selected client is compatible with the configured cluster versions (kubectl supports a one-minor skew). If the package catalog cannot supply the target version, report the blocker instead of editing generated Nix store paths by hand. Review the diff for unrelated package updates.

9. **Evaluate template compatibility guards** in `test/e2e/data/capx/cluster-template*.yaml`. The `VERSION_TO_COMPARE=1.29.0` checks select kube-vip's `admin.conf`/`super-admin.conf` mount around kubeadm initialization. They are behavior thresholds, not cluster version pins: preserve them unless upstream behavior requires changing the condition, and verify both pre- and post-kubeadm commands still apply to the target release.

10. **Search for any remaining references** across the repo (including hidden CI/skill files and nested modules). Look beyond the previous root-module minor: E2E and tooling may start at different versions. Update active pins where supported; retain historical notes, examples, and compatibility thresholds when appropriate:
   ```
   rg --hidden -n 'KUBERNETES_VERSION|kube-v1\.|kindest/node|VERSION_TO_COMPARE|k8s\.io/|kubectl' \
     --glob '!.git/**' --glob '!vendor/**' --glob '!**/go.sum'
   ```
