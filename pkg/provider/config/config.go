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

	credentialTypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	"k8s.io/klog/v2"
)

// Config of Nutanix provider
type Config struct {
	PrismCentral         credentialTypes.NutanixPrismEndpoint `json:"prismCentral"`
	TopologyDiscovery    TopologyDiscovery                    `json:"topologyDiscovery"`
	EnableCustomLabeling bool                                 `json:"enableCustomLabeling"`
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

func NewConfigFromBytes(bytes []byte) (Config, error) {
	nutanixConfig := Config{}
	if err := json.Unmarshal(bytes, &nutanixConfig); err != nil {
		return nutanixConfig, err
	}
	switch nutanixConfig.TopologyDiscovery.Type {
	case PrismTopologyDiscoveryType:
		return nutanixConfig, nil
	case "":
		klog.Warningf("topology discovery type was not set. Defaulting to %s", PrismTopologyDiscoveryType)
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
