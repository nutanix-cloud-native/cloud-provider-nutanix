package constants

const (
	ProviderName string = "nutanix"
	ClientName   string = "nutanix-cloud-controller-manager"

	DefaultCCMSecretNamespace string = "kube-system"

	InstanceType string = "ahv-vm"

	PoweredOffState string = "OFF"
	PoweredOnState  string = "ON"

	CustomPEUUIDLabel   string = "nutanix.com/prism-element-uuid"
	CustomPENameLabel   string = "nutanix.com/prism-element-name"
	CustomHostUUIDLabel string = "nutanix.com/prism-host-uuid"
	CustomHostNameLabel string = "nutanix.com/prism-host-name"
)
