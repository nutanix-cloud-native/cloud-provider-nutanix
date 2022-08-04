package mock

import (
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

type MockClient struct {
	mockPrism       MockPrism
	sharedInformers informers.SharedInformerFactory
	secretInformer  coreinformers.SecretInformer
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
