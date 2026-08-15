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

package mock

import (
	"context"
	"fmt"

	"github.com/nutanix-cloud-native/prism-go-client/converged"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	multidomainCommonModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/common/v1/config"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"k8s.io/utils/ptr"
)

type MockPrism struct {
	mockEnvironment                MockEnvironment
	IsProjectScopedOverride        func(ctx context.Context) bool
	GetDefaultProjectExtIdOverride func(ctx context.Context) *string
	GetClusterOverride             func(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error)
	ListAllClusterOverride         func(ctx context.Context) ([]clusterModels.Cluster, error)
	GetVMByBiosUUidOverride        func(ctx context.Context, biosUUID string) (*vmmModels.Vm, error)
	GetPrismCentralVersionOverride func(ctx context.Context) (string, error)
	GetResourceGroupsOverride      func(ctx context.Context) ([]multidomainModels.ResourceGroup, error)
	ListDomainManagersOverride     func(ctx context.Context) ([]prismModels.DomainManager, error)
}

func (mp *MockPrism) IsProjectScoped(ctx context.Context) bool {
	if mp.IsProjectScopedOverride != nil {
		return mp.IsProjectScopedOverride(ctx)
	}
	return false
}

func (mp *MockPrism) IsProjects20Supported(ctx context.Context) bool {
	return false
}

func (mp *MockPrism) GetVM(ctx context.Context, vmUUID string) (*vmmModels.Vm, error) {
	if v, ok := mp.mockEnvironment.managedMockMachines[vmUUID]; ok {
		return v, nil
	}
	return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("%s", vmNotFoundError)}
}

func (mp *MockPrism) GetVMByBiosUUid(ctx context.Context, biosUUID string) (*vmmModels.Vm, error) {
	if mp.GetVMByBiosUUidOverride != nil {
		return mp.GetVMByBiosUUidOverride(ctx, biosUUID)
	}
	if v, ok := mp.mockEnvironment.managedMockMachines[biosUUID]; ok {
		return v, nil
	}
	return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("%s", vmNotFoundError)}
}

func (mp *MockPrism) GetCluster(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error) {
	if mp.GetClusterOverride != nil {
		return mp.GetClusterOverride(ctx, clusterUUID)
	}
	return mp.mockEnvironment.managedMockClusters[clusterUUID], nil
}

func (mp *MockPrism) ListAllCluster(ctx context.Context) ([]clusterModels.Cluster, error) {
	if mp.ListAllClusterOverride != nil {
		return mp.ListAllClusterOverride(ctx)
	}
	entities := make([]clusterModels.Cluster, 0)

	for _, e := range mp.mockEnvironment.managedMockClusters {
		entities = append(entities, *e)
	}
	return entities, nil
}

func (mp *MockPrism) ListAllProject(ctx context.Context) ([]multidomainModels.Project, error) {
	return []multidomainModels.Project{}, nil
}

func (mp *MockPrism) GetDefaultProject(ctx context.Context) (*multidomainModels.Project, error) {
	return nil, nil
}

func (mp *MockPrism) GetDefaultProjectExtId(ctx context.Context) *string {
	if mp.GetDefaultProjectExtIdOverride != nil {
		return mp.GetDefaultProjectExtIdOverride(ctx)
	}
	return nil
}

func (mp *MockPrism) GetResourceGroups(ctx context.Context) ([]multidomainModels.ResourceGroup, error) {
	if mp.GetResourceGroupsOverride != nil {
		return mp.GetResourceGroupsOverride(ctx)
	}

	clusterNameCapability := multidomainCommonModels.NewOneOfKVPairValue()
	if err := clusterNameCapability.SetValue(MockCluster); err != nil {
		panic(err)
	}

	return []multidomainModels.ResourceGroup{
		{
			ExtId:        ptr.To("rg-1"),
			ProjectExtId: ptr.To("project-1"),
			PlacementTargets: []multidomainModels.TargetDetails{
				{
					ClusterExtId: ptr.To(MockClusterUUID),
					Capabilities: []multidomainCommonModels.KVPair{
						{
							Name:  ptr.To("cluster_name"),
							Value: clusterNameCapability,
						},
					},
				},
			},
		},
	}, nil
}

func (mp *MockPrism) ListDomainManagers(ctx context.Context) ([]prismModels.DomainManager, error) {
	if mp.ListDomainManagersOverride != nil {
		return mp.ListDomainManagersOverride(ctx)
	}
	return []prismModels.DomainManager{
		{
			Config: &prismModels.DomainManagerClusterConfig{
				Name: ptr.To(MockPrismCentral),
			},
		},
	}, nil
}

func (mp *MockPrism) GetCategory(ctx context.Context, categoryUUID string) (*prismModels.Category, error) {
	if cat, ok := mp.mockEnvironment.managedMockCategories[categoryUUID]; ok {
		return cat, nil
	}
	return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("%s", entityNotFoundError)}
}

func (mp *MockPrism) GetClusterHost(ctx context.Context, clusterUuid string, hostUUID string) (*clusterModels.Host, error) {
	if host, ok := mp.mockEnvironment.managedMockHosts[hostUUID]; ok {
		return host, nil
	}
	return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("%s", entityNotFoundError)}
}

func (mp *MockPrism) GetPrismCentralVersion(ctx context.Context) (string, error) {
	if mp.GetPrismCentralVersionOverride != nil {
		return mp.GetPrismCentralVersionOverride(ctx)
	}
	return MockPrismCentralVersion, nil
}
