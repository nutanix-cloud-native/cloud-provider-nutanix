package config

// Config of Nutanix provider
type Config struct {
	PrismCentral       NutanixPrismEndpoint `json:"prismCentral"`
	TopologyCategories *TopologyCategories  `json:"topologyCategories"`
}

type TopologyCategories struct {
	Zone   string `json:"zone"`
	Region string `json:"region"`
}

type NutanixPrismEndpoint struct {
	// address is the endpoint address (DNS name or IP address) of the Nutanix Prism Central or Element (cluster)
	Address string `json:"address"`

	// port is the port number to access the Nutanix Prism Central or Element (cluster)
	Port int32 `json:"port"`

	// use insecure connection to Prism endpoint
	// +optional
	Insecure bool `json:"insecure"`

	// Pass credential information for the target Prism instance
	// +optional
	CredentialRef *NutanixCredentialReference `json:"credentialRef,omitempty"`
}

type NutanixCredentialKind string

var SecretKind = NutanixCredentialKind("Secret")

type NutanixCredentialReference struct {
	// Kind of the Nutanix credential
	Kind NutanixCredentialKind `json:"kind"`

	// Name of the credential.
	Name string `json:"name"`
	// namespace of the credential.
	Namespace string `json:"namespace"`
}
