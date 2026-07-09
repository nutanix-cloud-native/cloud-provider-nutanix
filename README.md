# Nutanix Cloud Controller Manager

This repository contains the Kubernetes cloud-controller-manager for Nutanix AHV.

## Installation

**Note**: Requires a Kubernetes cluster that is configured for an external CCM.

The CCM is published as a Helm chart (OCI artifact) to GitHub Container Registry (GHCR).
To install it into the `kube-system` namespace:

```console
helm install nutanix-ccm oci://ghcr.io/nutanix-cloud-native/chart/nutanix-cloud-provider \
  --version <chart version (e.g. 0.6.5)> -n kube-system \
  --set prismCentralEndPoint=<PC IP/FQDN> \
  --set prismCentralInsecure=<true|false> \
  --set username=<PC username> \
  --set password=<PC password>
```

See the [chart README](charts/nutanix-cloud-provider/README.md) for the full list of
configurable values and additional examples.

## Developer Workflow

### Build the image
```
make docker-build
```

### Build and push the image
```
IMG=<image_name> make docker-push 
```

### Deploy CCM

**Note**: Requires a Kubernetes cluster that is configured for an external CCM

Make sure following environment variables are set before running `make deploy`:

- NUTANIX_ENDPOINT: Prism Central IP/FQDN
- NUTANIX_PORT: Prism Central Port (9440)
- NUTANIX_INSECURE: Disable certificate verification (true or false)
- NUTANIX_USERNAME: Username to connect to Prism Central 
- NUTANIX_PASSWORD: Password required to connect to Prism Central
- IMG: image name of Nutanix CCM 

```
IMG=<image_name> make deploy
```

The applied deployment manifests can be found in `_artifacts/manifests` after running `make deploy`. 

## Contributing
See the [contributing docs](CONTRIBUTING.md).

## Support
### Community Plus

This code is developed in the open with input from the community through issues and PRs. A Nutanix engineering team serves as the maintainer. Documentation is available in the project repository.

Issues and enhancement requests can be submitted in the [Issues tab of this repository](../../issues). Please search for and review the existing open issues before submitting a new issue.

## License
The project is released under version 2.0 of the [Apache license](http://www.apache.org/licenses/LICENSE-2.0).



