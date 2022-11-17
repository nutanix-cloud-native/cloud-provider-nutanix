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

	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
)

type MockPrism struct {
	mockEnvironment MockEnvironment
}

func (mp *MockPrism) GetVM(ctx context.Context, vmUUID string) (*prismClientV3.VMIntentResponse, error) {
	if v, ok := mp.mockEnvironment.managedMockMachines[vmUUID]; ok {
		return v, nil
	} else {
		return nil, fmt.Errorf(entityNotFoundError)
	}
}

func (mp *MockPrism) GetCluster(ctx context.Context, clusterUUID string) (*prismClientV3.ClusterIntentResponse, error) {
	return mp.mockEnvironment.managedMockClusters[clusterUUID], nil
}

func (mp *MockPrism) ListAllCluster(ctx context.Context, filter string) (*prismClientV3.ClusterListIntentResponse, error) {
	entities := make([]*prismClientV3.ClusterIntentResponse, 0)

	for _, e := range mp.mockEnvironment.managedMockClusters {
		entities = append(entities, e)
	}
	return &prismClientV3.ClusterListIntentResponse{
		Entities: entities,
	}, nil
}
