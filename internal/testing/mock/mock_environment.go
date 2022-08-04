package mock

import (
	"context"

	"github.com/nutanix-cloud-native/prism-go-client/utils"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
)

type MockEnvironment struct {
	managedMockMachines map[string]*prismClientV3.VMIntentResponse
	managedMockClusters map[string]*prismClientV3.ClusterIntentResponse
	managedNodes        map[string]*v1.Node
}

func (m *MockEnvironment) GetVM(vmName string) *prismClientV3.VMIntentResponse {
	for _, v := range m.managedMockMachines {
		if *v.Spec.Name == vmName {
			return v
		}
	}
	return nil
}

func (m *MockEnvironment) GetNode(nodeName string) *v1.Node {
	if n, ok := m.managedNodes[nodeName]; ok {
		return n
	}
	return nil
}

func (m *MockEnvironment) GetCluster(clusterName string) *prismClientV3.ClusterIntentResponse {
	for _, v := range m.managedMockClusters {
		if v.Spec.Name == &clusterName {
			return v
		}
	}
	return nil
}

func CreateMockEnvironment(ctx context.Context, kClient *fake.Clientset) (*MockEnvironment, error) {
	cluster := getDefaultClusterSpec(MockCluster)

	clusterCategories := getDefaultClusterSpec(mockClusterCategories)
	clusterCategories.Metadata.Categories[MockDefaultRegion] = MockRegion
	clusterCategories.Metadata.Categories[MockDefaultZone] = MockZone

	poweredOnVM := getDefaultVMSpec(MockVMNamePoweredOn, cluster)
	poweredOnNode, err := createNodeForVM(ctx, kClient, poweredOnVM)
	if err != nil {
		return nil, err
	}

	poweredOffVM := getDefaultVMSpec(MockVMNamePoweredOff, cluster)
	// PoweredOff Vms do not have host ref
	poweredOffVM.Status.Resources.HostReference = &prismClientV3.Reference{}
	poweredOffVM.Spec.Resources.PowerState = utils.StringPtr(constants.PoweredOffState)
	poweredOffNode, err := createNodeForVM(ctx, kClient, poweredOffVM)
	if err != nil {
		return nil, err
	}

	categoriesVM := getDefaultVMSpec(MockVMNameCategories, cluster)
	categoriesVM.Metadata.Categories[MockDefaultRegion] = MockRegion
	categoriesVM.Metadata.Categories[MockDefaultZone] = MockZone
	categoriesNode, err := createNodeForVM(ctx, kClient, categoriesVM)
	if err != nil {
		return nil, err
	}

	noAddressesVM := getDefaultVMSpec(MockVMNameNoAddresses, cluster)
	noAddressesVM.Status.Resources.NicList = []*prismClientV3.VMNicOutputStatus{}
	noAddressesNode, err := createNodeForVM(ctx, kClient, noAddressesVM)
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

	poweredOnVMClusterCategories := getDefaultVMSpec(MockVMNamePoweredOnClusterCategories, clusterCategories)
	poweredOnClusterCategoriesNode, err := createNodeForVM(ctx, kClient, poweredOnVMClusterCategories)
	if err != nil {
		return nil, err
	}

	return &MockEnvironment{
		managedMockMachines: map[string]*prismClientV3.VMIntentResponse{
			*poweredOnVM.Metadata.UUID:                  poweredOnVM,
			*poweredOffVM.Metadata.UUID:                 poweredOffVM,
			*categoriesVM.Metadata.UUID:                 categoriesVM,
			*noAddressesVM.Metadata.UUID:                noAddressesVM,
			*poweredOnVMClusterCategories.Metadata.UUID: poweredOnVMClusterCategories,
		},
		managedMockClusters: map[string]*prismClientV3.ClusterIntentResponse{
			*cluster.Metadata.UUID:           cluster,
			*clusterCategories.Metadata.UUID: clusterCategories,
		},
		managedNodes: map[string]*v1.Node{
			MockVMNamePoweredOn:                  poweredOnNode,
			MockVMNamePoweredOff:                 poweredOffNode,
			MockVMNameCategories:                 categoriesNode,
			MockVMNameNoAddresses:                noAddressesNode,
			MockNodeNameVMNotExisting:            nonExistingVMNode,
			MockNodeNameNoSystemUUID:             noSystemUUIDNode,
			MockVMNamePoweredOnClusterCategories: poweredOnClusterCategoriesNode,
		},
	}, nil
}
