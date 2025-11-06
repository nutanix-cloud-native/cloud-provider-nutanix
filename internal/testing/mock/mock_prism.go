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

	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
)

type MockPrism struct {
	mockEnvironment MockEnvironment
}

func (mp *MockPrism) GetVM(ctx context.Context, vmUUID string) (*vmmModels.Vm, error) {
	if v, ok := mp.mockEnvironment.managedMockMachines[vmUUID]; ok {
		return v, nil
	} else {
		return nil, fmt.Errorf(vmNotFoundError)
	}
}

func (mp *MockPrism) GetCluster(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error) {
	return mp.mockEnvironment.managedMockClusters[clusterUUID], nil
}

func (mp *MockPrism) ListAllCluster(ctx context.Context) ([]clusterModels.Cluster, error) {
	entities := make([]clusterModels.Cluster, 0)

	for _, e := range mp.mockEnvironment.managedMockClusters {
		entities = append(entities, *e)
	}
	return entities, nil
}

func (mp *MockPrism) GetCategory(ctx context.Context, categoryUUID string) (*prismModels.Category, error) {
	if cat, ok := mp.mockEnvironment.managedMockCategories[categoryUUID]; ok {
		return cat, nil
	}
	return nil, fmt.Errorf(entityNotFoundError)
}

func (mp *MockPrism) GetClusterHost(ctx context.Context, clusterUuid string, hostUUID string) (*clusterModels.Host, error) {
	if host, ok := mp.mockEnvironment.managedMockHosts[hostUUID]; ok {
		return host, nil
	}
	return nil, fmt.Errorf(entityNotFoundError)
}
