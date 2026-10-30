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

package constants

const (
	ProviderName string = "nutanix"
	ClientName   string = "nutanix-cloud-controller-manager"

	CCMNamespaceKey = "POD_NAMESPACE"

	InstanceType string = "ahv-vm"

	PoweredOffState string = "OFF"
	PoweredOnState  string = "ON"

	PEUUIDLabel                    string = "nutanix.com/prism-element-uuid"
	PENameLabel                    string = "nutanix.com/prism-element-name"
	HostUUIDLabel                  string = "nutanix.com/prism-host-uuid"
	HostNameLabel                  string = "nutanix.com/prism-host-name"
	MetroNodeGroupLabel            string = "nutanix.com/metro-site-group"
	MetroNodeGroupNameAttributeKey string = "metro-node-group-name"
	FailureDomainAttributeKey      string = "failure-domain"

	ProjectUUIDLabel       string = "nutanix.com/project-uuid"
	ResourceGroupUUIDLabel string = "nutanix.com/resource-group-uuid"
	PrismCentralService    string = "PRISM_CENTRAL"

	// ResourceGroupClusterNameCapabilityKey is the capability name on a resource group's
	// placement target that carries the target cluster's name. Project-scoped Prism clients
	// (PC 7.6+) cannot call cluster-wide APIs, so this is how PE name is resolved for them.
	ResourceGroupClusterNameCapabilityKey string = "cluster_name"

	// LoadBalancerStrategyAnnotation is set on a Service to override the CCM
	// default LoadBalancer strategy for that Service. The value must be a
	// registered strategy name (e.g. "None", "AutoIPAMMetalLB").
	LoadBalancerStrategyAnnotation string = "ccm.nutanix.com/load-balancer-strategy"

	// LoadBalancerReservationIDAnnotation is the NTNX-Request-Id used to reserve
	// this Service's address. It is written before the Prism call.
	LoadBalancerReservationIDAnnotation string = "ccm.nutanix.com/load-balancer-reservation-id"
	// LoadBalancerIPAnnotation is the address this strategy reserved for the Service.
	LoadBalancerIPAnnotation string = "ccm.nutanix.com/load-balancer-ip"
	// LoadBalancerSubnetAnnotation is the Prism subnet extID the address was reserved on.
	LoadBalancerSubnetAnnotation string = "ccm.nutanix.com/load-balancer-subnet"
	// MetalLBRequestedIPsAnnotation asks MetalLB to announce exactly these addresses.
	MetalLBRequestedIPsAnnotation string = "metallb.universe.tf/loadBalancerIPs"

	// LoadBalancerServiceUIDLabel identifies the Service that owns an IPAddressPool.
	LoadBalancerServiceUIDLabel string = "ccm.nutanix.com/load-balancer-service-uid"
	// LoadBalancerReservationIDLabel is the reservation id copied onto the IPAddressPool.
	LoadBalancerReservationIDLabel string = LoadBalancerReservationIDAnnotation
	// LoadBalancerIPLabel is the reserved address copied onto the IPAddressPool.
	LoadBalancerIPLabel string = LoadBalancerIPAnnotation
	// LoadBalancerSubnetLabel is the Prism subnet extID copied onto the IPAddressPool.
	LoadBalancerSubnetLabel string = LoadBalancerSubnetAnnotation
	// LoadBalancerPoolTypeLabel selects AutoIPAMMetalLB pools from an L2Advertisement
	// ipAddressPoolSelectors match.
	LoadBalancerPoolTypeLabel string = "pool-type"
	// LoadBalancerPoolTypeAutoIPAMMetalLB is the pool-type value for AutoIPAMMetalLB.
	LoadBalancerPoolTypeAutoIPAMMetalLB string = "auto-ipam-metallb"
)
