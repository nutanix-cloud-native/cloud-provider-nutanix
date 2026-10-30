# AutoIPAM MetalLB PC integration tests

These tests start envtest, run the cloud-provider Service controller with
`NtnxCloud` (`loadBalancer.defaultStrategy: AutoIPAMMetalLB`), and call Prism
Central `ReserveIP` for real. MetalLB is not installed; only the `IPAddressPool`
CRD is loaded.

They are not part of `make unit-test`.

## Requirements

- Go toolchain for this repo
- Network access to a Prism Central that can reserve addresses
- envtest binaries via `setup-envtest` (resolved by `make integration-test`)

## Environment

Required:

| Variable | Description |
| --- | --- |
| `NUTANIX_ENDPOINT` | Prism Central address |
| `NUTANIX_SUBNET_NAME` | Prism subnet name (resolved to extID for AutoIPAMMetalLB) |
| Credentials | `NUTANIX_API_KEY`, or `NUTANIX_USER` / `NUTANIX_USERNAME` + `NUTANIX_PASSWORD` |

Optional:

| Variable | Default | Description |
| --- | --- | --- |
| `NUTANIX_PORT` | `9440` | Prism Central port |
| `NUTANIX_INSECURE` | `true` | Skip TLS verify when `true` |
| `NUTANIX_AUTOIPAM_METALLB_NAMESPACE` | `metallb-system` | Namespace for per-Service pools |

## Run

```bash
export NUTANIX_ENDPOINT=...
export NUTANIX_USER=...
export NUTANIX_PASSWORD=...
export NUTANIX_SUBNET_NAME=...
make integration-test
```

Or:

```bash
KUBEBUILDER_ASSETS="$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22 use 1.36 -p path)" \
  go test -tags=integration -count=1 -timeout=30m -v ./test/integration/...
```

If required variables are missing, the suite skips.

## Cleanup

Product unreserve is out of scope. Each test registers harness cleanup that calls
Prism `UnreserveIpsBySubnetId` by Service UID, deletes the pool, strips
`service.kubernetes.io/load-balancer-cleanup`, and deletes the Service.
