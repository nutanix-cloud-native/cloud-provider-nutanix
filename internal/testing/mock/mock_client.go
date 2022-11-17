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
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

type MockClient struct {
	mockPrism         MockPrism
	sharedInformers   informers.SharedInformerFactory
	secretInformer    coreinformers.SecretInformer
	configMapInformer coreinformers.ConfigMapInformer
}

func CreateMockClient(mockEnvironment MockEnvironment) *MockClient {
	return &MockClient{
		mockPrism: MockPrism{
			mockEnvironment: mockEnvironment,
		},
	}
}

func (mc *MockClient) Get() (interfaces.Prism, error) {
	return &mc.mockPrism, nil
}

func (mc *MockClient) SetInformers(sharedInformers informers.SharedInformerFactory) {
	mc.sharedInformers = sharedInformers
}
