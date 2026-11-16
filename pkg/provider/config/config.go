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
	klog "k8s.io/klog/v2"
)

// Config of Nutanix provider
type Config struct {
	PrismCentral         credentialTypes.NutanixPrismEndpoint `json:"prismCentral"`
	TopologyDiscovery    TopologyDiscovery                    `json:"topologyDiscovery"`
	EnableCustomLabeling bool                                 `json:"enableCustomLabeling"`
	IgnoredNodeIPs       []string                             `json:"ignoredNodeIPs,omitempty"`
	LoadBalancer         LoadBalancer                         `json:"loadBalancer,omitempty"`
}

// LoadBalancer holds configuration for the CCM Service LoadBalancer controller.
type LoadBalancer struct {
	// DefaultStrategy is the LoadBalancer strategy applied to a Service that
	// does not carry the strategy override annotation. When empty it defaults
	// to the "None" (do-nothing) strategy, preserving legacy static-pool
	// behaviour where MetalLB owns allocation and advertisement.
	DefaultStrategy string `json:"defaultStrategy,omitempty"`
	// AutoIPAMMetalLB configures the AutoIPAMMetalLB strategy. None does not use it.
	AutoIPAMMetalLB AutoIPAMMetalLB `json:"autoIPAMMetalLB,omitempty"`
}

// AutoIPAMMetalLB configures reservation of one Prism address per Service and
// the MetalLB pool that announces it.
type AutoIPAMMetalLB struct {
	// Subnet is the Prism subnet addresses are reserved from, either an extID or
	// a subnet name. Empty makes activation fail before any Prism or MetalLB
	// write.
	Subnet string `json:"subnet,omitempty"`
	// Cluster narrows a Subnet given by name to one Prism Element, by cluster
	// name or extID. It is only consulted when Subnet is a name, and only for
	// subnets that declare a cluster.
	Cluster string `json:"cluster,omitempty"`
	// ClusterUID is the UID of this workload cluster's CAPI Cluster object. It
	// is half of the reservation identity Prism stores, which is what lets the
	// management cluster release this cluster's addresses after the workload
	// cluster is gone. The CCM cannot derive it, so activation fails when it is
	// empty.
	ClusterUID string `json:"clusterUID,omitempty"`
	// MetalLBNamespace is the namespace of the per-Service IPAddressPool.
	// Empty defaults to metallb-system.
	MetalLBNamespace string `json:"metalLBNamespace,omitempty"`
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
