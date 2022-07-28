package provider

const (
	ProviderName string = "nutanix"
	ClientName   string = "nutanix-cloud-controller-manager"

	defaultCCMSecretNamespace string = "kube-system"

	instanceType string = "ahv-vm"

	poweredOffState string = "OFF"

	customPEUUIDLabel   string = "nutanix.com/prism-element-uuid"
	customPENameLabel   string = "nutanix.com/prism-element-name"
	customHostUUIDLabel string = "nutanix.com/prism-host-uuid"
	customHostNameLabel string = "nutanix.com/prism-host-name"
)
