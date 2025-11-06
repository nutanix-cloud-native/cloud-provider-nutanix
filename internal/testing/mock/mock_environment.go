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

//nolint:typecheck // Mock file uses ginkgo/gomega which typecheck doesn't understand well
package mock

import (
	"context"

	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmCommonModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/common/v1/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

type MockEnvironment struct {
	managedMockMachines   map[string]*vmmModels.Vm
	managedMockClusters   map[string]*clusterModels.Cluster
	managedMockHosts      map[string]*clusterModels.Host
	managedMockCategories map[string]*prismModels.Category
	managedNodes          map[string]*v1.Node
	vmNameToExtId         map[string]string
}

func (m *MockEnvironment) GetVM(ctx context.Context, vmName string) *vmmModels.Vm {
	if extId, ok := m.vmNameToExtId[vmName]; ok {
		return m.managedMockMachines[extId]
	}
	return nil
}

func (m *MockEnvironment) GetNode(nodeName string) *v1.Node {
	if n, ok := m.managedNodes[nodeName]; ok {
		return n
	}
	return nil
}

func (m *MockEnvironment) GetCluster(ctx context.Context, clusterName string) *clusterModels.Cluster {
	for _, v := range m.managedMockClusters {
		if *v.Name == clusterName {
			return v
		}
	}
	return nil
}

func (m *MockEnvironment) AddCluster(cluster *clusterModels.Cluster) *clusterModels.Cluster {
	Expect(cluster).ToNot(BeNil()) // nolint:typecheck
	m.managedMockClusters[*cluster.ExtId] = cluster
	return nil
}

func (m *MockEnvironment) DeleteCluster(clusterUUID string) {
	Expect(clusterUUID).ToNot(BeEmpty()) // nolint:typecheck
	delete(m.managedMockClusters, clusterUUID)
}

func CreateMockEnvironment(ctx context.Context, kClient *fake.Clientset) (*MockEnvironment, error) {
	// Create clusters with consistent UUIDs
	cluster := getDefaultCluster(MockCluster, MockClusterUUID)
	pc := CreatePrismCentralCluster(MockPrismCentral, MockPrismCentralUUID)
	clusterCategories := getDefaultCluster(mockClusterCategories, MockClusterCategoriesUUID)

	// Create host with consistent UUID
	host := getDefaultHost(mockHost, MockHostUUID, MockClusterUUID)

	// Create categories with consistent UUIDs
	regionCategory := getDefaultCategory(MockDefaultRegion, MockCategoryRegionUUID, MockRegion)
	zoneCategory := getDefaultCategory(MockDefaultZone, MockCategoryZoneUUID, MockZone)

	// Create VMs with consistent UUIDs
	poweredOnVM := getDefaultVM(MockVMNamePoweredOn, MockVMPoweredOnUUID, cluster, host)
	poweredOnNode, err := createNodeForVM(ctx, kClient, poweredOnVM)
	if err != nil {
		return nil, err
	}

	poweredOffVM := getDefaultVM(MockVMNamePoweredOff, MockVMPoweredOffUUID, cluster, nil) // PoweredOff VMs do not have host ref
	poweredOffVM.PowerState = vmmModels.POWERSTATE_OFF.Ref()
	poweredOffVM.Cluster = &vmmModels.ClusterReference{
		ExtId: ptr.To(MockClusterUUID),
	}
	poweredOffNode, err := createNodeForVM(ctx, kClient, poweredOffVM)
	if err != nil {
		return nil, err
	}

	categoriesVM := getDefaultVM(MockVMNameCategories, MockVMCategoriesUUID, cluster, host)
	categoriesVM.Categories = []vmmModels.CategoryReference{
		{ExtId: regionCategory.ExtId},
		{ExtId: zoneCategory.ExtId},
	}
	categoriesNode, err := createNodeForVM(ctx, kClient, categoriesVM)
	if err != nil {
		return nil, err
	}

	noAddressesVM := getDefaultVM(MockVMNameNoAddresses, MockVMNoAddressesUUID, cluster, host)
	noAddressesVM.Nics = nil
	noAddressesNode, err := createNodeForVM(ctx, kClient, noAddressesVM)
	if err != nil {
		return nil, err
	}

	filteredAddressesVM := getDefaultVM(MockVMNameFilteredNodeAddresses, MockVMFilteredAddressesUUID, cluster, host)
	// Create multiple NICs with different IPs
	filteredNics := make([]vmmModels.Nic, 0)
	ipAddresses := []string{"127.100.10.1", "127.200.20.1", "127.200.100.64", "127.200.200.10", MockIP}
	for _, ip := range ipAddresses {
		nic := vmmModels.NewNic()
		nicNetInfo := vmmModels.NewVirtualEthernetNicNetworkInfo()
		nicNetInfo.Ipv4Config = vmmModels.NewIpv4Config()
		nicNetInfo.Ipv4Config.IpAddress = &vmmCommonModels.IPv4Address{
			Value: ptr.To(ip),
		}
		nicNetInfo.Ipv4Info = vmmModels.NewIpv4Info()
		nic.SetNicNetworkInfo(*nicNetInfo)
		filteredNics = append(filteredNics, *nic)
	}
	filteredAddressesVM.Nics = filteredNics
	filteredAddressesNode, err := createNodeForVM(ctx, kClient, filteredAddressesVM)
	if err != nil {
		return nil, err
	}

	nonExistingVMNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: MockNodeNameVMNotExisting,
		},
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				SystemUUID: MockNodeNameVMNotExisting,
			},
		},
	}

	noSystemUUIDNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: MockNodeNameNoSystemUUID,
		},
	}

	poweredOnVMClusterCategories := getDefaultVM(MockVMNamePoweredOnClusterCategories, MockVMPoweredOnClusterCategoriesUUID, clusterCategories, host)
	poweredOnVMClusterCategories.Categories = []vmmModels.CategoryReference{
		{ExtId: regionCategory.ExtId},
		{ExtId: zoneCategory.ExtId},
	}
	poweredOnClusterCategoriesNode, err := createNodeForVM(ctx, kClient, poweredOnVMClusterCategories)
	if err != nil {
		return nil, err
	}

	dpOffloadVM := getDefaultVMWithDpOffload(MockVMNameDpOffload, MockVMDpOffloadUUID, cluster, host)
	dpOffloadNode, err := createNodeForVM(ctx, kClient, dpOffloadVM)
	if err != nil {
		return nil, err
	}

	secondaryIPsVM := getDefaultVMWithSecondaryIPs(MockVMNameSecondaryIPs, MockVMSecondaryIPsUUID, cluster, host)
	secondaryIPsNode, err := createNodeForVM(ctx, kClient, secondaryIPsVM)
	if err != nil {
		return nil, err
	}

	return &MockEnvironment{
		managedMockMachines: map[string]*vmmModels.Vm{
			*poweredOnVM.ExtId:                  poweredOnVM,
			*poweredOffVM.ExtId:                 poweredOffVM,
			*categoriesVM.ExtId:                 categoriesVM,
			*noAddressesVM.ExtId:                noAddressesVM,
			*poweredOnVMClusterCategories.ExtId: poweredOnVMClusterCategories,
			*filteredAddressesVM.ExtId:          filteredAddressesVM,
			*dpOffloadVM.ExtId:                  dpOffloadVM,
			*secondaryIPsVM.ExtId:               secondaryIPsVM,
		},
		managedMockClusters: map[string]*clusterModels.Cluster{
			*cluster.ExtId:           cluster,
			*clusterCategories.ExtId: clusterCategories,
			*pc.ExtId:                pc,
		},
		managedMockHosts: map[string]*clusterModels.Host{
			*host.ExtId: host,
		},
		managedMockCategories: map[string]*prismModels.Category{
			*regionCategory.ExtId: regionCategory,
			*zoneCategory.ExtId:   zoneCategory,
		},
		managedNodes: map[string]*v1.Node{
			MockVMNamePoweredOn:                  poweredOnNode,
			MockVMNamePoweredOff:                 poweredOffNode,
			MockVMNameCategories:                 categoriesNode,
			MockVMNameNoAddresses:                noAddressesNode,
			MockNodeNameVMNotExisting:            nonExistingVMNode,
			MockNodeNameNoSystemUUID:             noSystemUUIDNode,
			MockVMNamePoweredOnClusterCategories: poweredOnClusterCategoriesNode,
			MockVMNameFilteredNodeAddresses:      filteredAddressesNode,
			MockVMNameDpOffload:                  dpOffloadNode,
			MockVMNameSecondaryIPs:               secondaryIPsNode,
		},
		vmNameToExtId: map[string]string{
			MockVMNamePoweredOn:                  *poweredOnVM.ExtId,
			MockVMNamePoweredOff:                 *poweredOffVM.ExtId,
			MockVMNameCategories:                 *categoriesVM.ExtId,
			MockVMNameNoAddresses:                *noAddressesVM.ExtId,
			MockVMNameFilteredNodeAddresses:      *filteredAddressesVM.ExtId,
			MockVMNamePoweredOnClusterCategories: *poweredOnVMClusterCategories.ExtId,
			MockVMNameDpOffload:                  *dpOffloadVM.ExtId,
			MockVMNameSecondaryIPs:               *secondaryIPsVM.ExtId,
		},
	}, nil
}
