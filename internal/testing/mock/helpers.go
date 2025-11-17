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

	"github.com/google/uuid"
	credentialTypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	"github.com/nutanix-cloud-native/prism-go-client/utils"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

func getDefaultVMSpec(vmName string, cluster *prismClientV3.ClusterIntentResponse) *prismClientV3.VMIntentResponse {
	vmUUID := uuid.New()
	hostUUID := uuid.New()
	vm := &prismClientV3.VMIntentResponse{
		Metadata: &prismClientV3.Metadata{
			UUID:       utils.StringPtr(vmUUID.String()),
			Categories: map[string]string{},
		},
		Spec: &prismClientV3.VM{
			Resources: &prismClientV3.VMResources{
				PowerState: utils.StringPtr(constants.PoweredOnState),
			},
			Name: utils.StringPtr(vmName),
		},
		Status: &prismClientV3.VMDefStatus{
			ClusterReference: &prismClientV3.Reference{},
			Resources: &prismClientV3.VMResourcesDefStatus{
				HostReference: &prismClientV3.Reference{
					UUID: utils.StringPtr(hostUUID.String()),
					Name: utils.StringPtr(mockHost),
				},
				NicList: []*prismClientV3.VMNicOutputStatus{
					{
						IPEndpointList: []*prismClientV3.IPAddress{
							{
								IP:   utils.StringPtr(MockIP),
								Type: utils.StringPtr("Assigned"),
							},
						},
					},
				},
			},
		},
	}
	if cluster != nil {
		vm.Status.ClusterReference.Name = utils.StringPtr(*cluster.Spec.Name)
		vm.Status.ClusterReference.UUID = utils.StringPtr(*cluster.Metadata.UUID)
	}
	return vm
}

func getDefaultClusterSpec(clusterName string) *prismClientV3.ClusterIntentResponse {
	id := uuid.New()
	return &prismClientV3.ClusterIntentResponse{
		Metadata: &prismClientV3.Metadata{
			UUID:       utils.StringPtr(id.String()),
			Categories: map[string]string{},
		},
		Spec: &prismClientV3.Cluster{
			Name: utils.StringPtr(clusterName),
		},
		Status: &prismClientV3.ClusterDefStatus{
			Resources: &prismClientV3.ClusterObj{
				Config: &prismClientV3.ClusterConfig{
					ServiceList: make([]*string, 0),
				},
			},
		},
	}
}

func createNodeForVM(ctx context.Context, kClient *fake.Clientset, vm *prismClientV3.VMIntentResponse) (*v1.Node, error) {
	n := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: *vm.Spec.Name,
		},

		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				SystemUUID: *vm.Metadata.UUID,
			},
		},
	}
	node, err := kClient.CoreV1().Nodes().Create(ctx, n, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return node, nil
}

func ValidateInstanceMetadata(metadata *cloudprovider.InstanceMetadata, vm *prismClientV3.VMIntentResponse, region, zone string) {
	Expect(metadata).NotTo(BeNil())
	Expect(metadata.InstanceType).To(Equal(constants.InstanceType))
	Expect(metadata.ProviderID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.Metadata.UUID)))
	Expect(metadata.Region).To(Equal(region))
	Expect(metadata.Zone).To(Equal(zone))
}

func GenerateMockConfig() config.Config {
	return config.Config{
		PrismCentral: credentialTypes.NutanixPrismEndpoint{
			Address:  mockAddress,
			Port:     mockPort,
			Insecure: mockInsecure,
			CredentialRef: &credentialTypes.NutanixCredentialReference{
				Kind:      credentialTypes.SecretKind,
				Name:      mockCredentialRef,
				Namespace: mockNamespace,
			},
		},
		TopologyDiscovery: config.TopologyDiscovery{
			Type: config.PrismTopologyDiscoveryType,
		},
	}
}

func CheckAdditionalLabels(node *v1.Node, vm *prismClientV3.VMIntentResponse) {
	Expect(vm).ToNot(BeNil())
	Expect(node).ToNot(BeNil())

	toMatchKeys := gstruct.Keys{
		constants.CustomPEUUIDLabel: Equal(*vm.Status.ClusterReference.UUID),
		constants.CustomPENameLabel: Equal(*vm.Status.ClusterReference.Name),
	}
	if vm.Status.Resources.HostReference != nil &&
		vm.Status.Resources.HostReference.UUID != nil &&
		vm.Status.Resources.HostReference.Name != nil {
		toMatchKeys[constants.CustomHostUUIDLabel] = Equal(*vm.Status.Resources.HostReference.UUID)
		toMatchKeys[constants.CustomHostNameLabel] = Equal(*vm.Status.Resources.HostReference.Name)
	}

	Expect(node.Labels).To(gstruct.MatchAllKeys(toMatchKeys))
}

func CreatePrismCentralCluster(clusterName string) *prismClientV3.ClusterIntentResponse {
	pc := getDefaultClusterSpec(clusterName)
	pc.Status.Resources.Config.ServiceList = []*string{
		utils.StringPtr(constants.PrismCentralService),
	}
	return pc
}
