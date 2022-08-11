/*
Copyright 2022 Nutanix, Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"
)

// Config of Nutanix provider
type Config struct {
	PrismCentral         NutanixPrismEndpoint `json:"prismCentral"`
	TopologyDiscovery    TopologyDiscovery    `json:"topologyDiscovery"`
	EnableCustomLabeling bool                 `json:"enableCustomLabeling"`
}

type TopologyDiscovery struct {
	// Default type will be set to Prism via the newConfig function
	Type               TopologyDiscoveryType `json:"type"`
	TopologyCategories *TopologyCategories   `json:"topologyCategories"`
}

type TopologyDiscoveryType string

const (
	PrismTopologyDiscoveryType      = TopologyDiscoveryType("Prism")
	CategoriesTopologyDiscoveryType = TopologyDiscoveryType("Categories")
)

type TopologyInfo struct {
	Zone   string `json:"zone"`
	Region string `json:"region"`
}

type TopologyCategories struct {
	ZoneCategory   string `json:"zoneCategory"`
	RegionCategory string `json:"regionCategory"`
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

func NewConfigFromBytes(bytes []byte) (Config, error) {
	nutanixConfig := Config{}
	if err := json.Unmarshal(bytes, &nutanixConfig); err != nil {
		return nutanixConfig, err
	}
	switch nutanixConfig.TopologyDiscovery.Type {
	case PrismTopologyDiscoveryType:
		return nutanixConfig, nil
	case "":
		klog.Warning("topology discovery type was not set. Defaulting to %s", PrismTopologyDiscoveryType)
		nutanixConfig.TopologyDiscovery.Type = PrismTopologyDiscoveryType
		return nutanixConfig, nil
	case CategoriesTopologyDiscoveryType:
		if nutanixConfig.TopologyDiscovery.TopologyCategories == nil {
			return nutanixConfig, fmt.Errorf("topologyCategories must be set when using topology discovery type: %s", CategoriesTopologyDiscoveryType)
		}
		return nutanixConfig, nil
	}
	return nutanixConfig, fmt.Errorf("unsupported topology discovery type: %s", nutanixConfig.TopologyDiscovery.Type)
}
