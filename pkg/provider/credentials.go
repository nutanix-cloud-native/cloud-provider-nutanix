package provider

import (
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

type CredentialType string

type NutanixCredentials struct {
	Credentials []Credential `json:"credentials"`
}

type Credential struct {
	Type CredentialType           `json:"type"`
	Data *k8sruntime.RawExtension `json:"data"`
}

type BasicAuthCredential struct {
	// The Basic Auth (username, password) for the Prism Central
	PrismCentral PrismCentralBasicAuth `json:"prismCentral"`

	// The Basic Auth (username, password) for the Prism Elements (clusters).
	PrismElements []PrismElementBasicAuth `json:"prismElements"`
}

type PrismCentralBasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PrismElementBasicAuth struct {
	// name is the unique resource name of the Prism Element (cluster) in the Prism Central's domain
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}
