# Run E2E Tests

Use this skill when you need to run CCM end-to-end tests in this repository.

1. Ensure prerequisites are available:
   - Docker is running (and already logged in to whatever registry you'll push the CCM image to, if it requires auth).
   - `ginkgo` is installed (`go install github.com/onsi/ginkgo/v2/ginkgo@latest`).
   - `ko` is installed (used by `docker-build`/`docker-push`, which `test-e2e` depends on — no need to build/push separately before `make test-e2e`).
   - Never call Prism Central v3 APIs for anything in this repo, including ad-hoc lookups — use v4 endpoints only.

2. Choose an image registry:
   - Default `LOCAL_IMAGE_REGISTRY` is `ko.local`, which only works when the cluster under test *is* the local kind bootstrap cluster (ko loads it straight into the local Docker daemon, not any real registry).
   - If the scenario provisions **real VMs** (e.g. CAPX against a live Prism Central), those nodes cannot resolve `ko.local` — override `LOCAL_IMAGE_REGISTRY` to a real, reachable registry you're already authenticated to, e.g. `LOCAL_IMAGE_REGISTRY=<host>/<path> make test-e2e ...`.

3. Configure E2E inputs:
   - Default config file is `test/e2e/config/nutanix.yaml`.
   - Base variables (commonly required): `NUTANIX_ENDPOINT`, `NUTANIX_USER`/`NUTANIX_PASSWORD` (or `NUTANIX_API_KEY` where supported), `NUTANIX_PRISM_ELEMENT_CLUSTER_NAME`, `NUTANIX_SUBNET_NAME`, `NUTANIX_MACHINE_TEMPLATE_IMAGE_NAME`, `NUTANIX_SSH_AUTHORIZED_KEY`, and `CONTROL_PLANE_ENDPOINT_IP`.
   - Optional connection/tuning variables: `NUTANIX_PORT`, `NUTANIX_INSECURE`, `NUTANIX_ADDITIONAL_TRUST_BUNDLE`, `NUTANIX_MACHINE_BOOT_TYPE`, `NUTANIX_MACHINE_MEMORY_SIZE`, `NUTANIX_MACHINE_VCPU_SOCKET`, `NUTANIX_MACHINE_VCPU_PER_SOCKET`, `NUTANIX_SYSTEMDISK_SIZE`.
   - CAPX scenarios (labels, see `test/e2e/capx_test.go`): `default-project` (baseline, non-project-scoped creds, default project — always runs), `non-default-project` (non-project-scoped creds, VM routed to a named non-default project; needs `NUTANIX_EXPECTED_PC_PROJECT`, optionally `NUTANIX_NON_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME`), `project-scoped-user` (a project-scoped PC user; needs `NUTANIX_PROJECT_SCOPED_USER`, `NUTANIX_PROJECT_SCOPED_PASSWORD`, `NUTANIX_PROJECT_SCOPED_PC_PROJECT`, optionally `NUTANIX_PROJECT_SCOPED_MACHINE_TEMPLATE_IMAGE_NAME`). The `non-default-project`/`project-scoped-user` scenarios resolve the project's and its resource-group's UUIDs themselves at runtime via the v4 API (from the project *name* you give them) — you do not need to pre-compute or pass in UUIDs.
   - For the project-targeted scenarios, the VM image must actually be visible/shared to the target PC project — an image that only exists in a different project (e.g. an internal/admin-only project) will fail VM provisioning. Confirm with whoever owns the PC environment which image is visible in the target project; don't assume an image valid for one project is valid for another.
   - `CONTROL_PLANE_ENDPOINT_IP` is a single global variable shared by every scenario in one invocation — if you run multiple scenario labels in the same process/sequence, they reuse the same IP one after another (each cluster is torn down before the next is created). There's no way to give each scenario its own distinct IP without a code change.
   - When adding node-label assertions to a new scenario, make sure the assertion actually matches what that scenario's cluster template enables — e.g. a host-UUID label assertion is only valid when that template sets `enableCustomLabeling: true`, since that label is opt-in. Don't "fix" a mismatch by forcing the template's flag on; fix whichever side (assertion or template) doesn't match the scenario's actual intent.
   - The CAPI version this suite pins (v1.11.2, see `test/e2e/config/nutanix.yaml`) auto-injects the kubeadm feature gate `ControlPlaneKubeletLocalMode` for any Kubernetes version >= v1.31.0, but Kubernetes v1.36.x kubeadm no longer accepts that gate name — `kubeadm init` fails immediately on any v1.36.x image with `ControlPlaneKubeletLocalMode is not a valid feature name`. Stick to Kubernetes v1.35.x (or earlier) images for this repo's E2E until the pinned CAPI version is bumped past whatever release fixes this.

4. Run the full suite with make:
   - `make test-e2e`

5. Run focused executions when needed:
   - Label-based filtering: `make test-e2e LABEL_FILTERS='<ginkgo label expression>'`
   - Parallel nodes: `make test-e2e GINKGO_NODES=2`
   - Custom config: `make test-e2e E2E_CONF_FILE=/absolute/path/to/config.yaml`
   - The Makefile hardcodes ginkgo's `--timeout` to `1h` for the whole suite (not overridable via a make variable). When running several heavyweight scenarios against real infrastructure, consider invoking each label as its own `make test-e2e LABEL_FILTERS='<one-label>'` run in sequence — each gets a fresh 1h budget and its own kind bootstrap cluster, instead of one combined run that can hit the aggregate timeout.

6. Run a compile/smoke check (no real E2E execution):
   - `go test -C test/e2e -tags=e2e -run '^$' .`

7. Review outputs:
   - Artifacts and junit report are written under `$(ARTIFACTS)` (see `JUNIT_REPORT_FILE` and ginkgo output settings in `Makefile`).
   - Per-run resource dumps (Cluster/Machine/NutanixMachine YAML, controller logs) land under `$(ARTIFACTS)/clusters/` — useful for diagnosing a failed scenario after the kind bootstrap cluster has already been torn down.
   - While a scenario is running against real infrastructure, actively inspect the live kind bootstrap cluster (`kind get kubeconfig --name <name>`, then `kubectl get clusters,kubeadmcontrolplanes,machines,nutanixmachines -A`) and, if needed, SSH into the workload VM itself (user is `capiuser` per the cluster templates' `sshAuthorizedKeys` block; check `journalctl -u kubelet` and `/var/log/cloud-init-output.log` for the real error) — don't just tail the ginkgo log and wait for its eventual timeout message, which is far less informative than the live object/VM state.
