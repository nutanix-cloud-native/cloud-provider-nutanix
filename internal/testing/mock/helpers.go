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
	"fmt"

	credentialTypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmCommonModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/common/v1/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants" //nolint:typecheck
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

func getDefaultVM(vmName string, vmUUID string, cluster *clusterModels.Cluster, host *clusterModels.Host) *vmmModels.Vm {
	nic := vmmModels.NewNic()
	nicNetInfo := vmmModels.NewVirtualEthernetNicNetworkInfo()

	nicNetInfo.Ipv4Config = vmmModels.NewIpv4Config()
	nicNetInfo.Ipv4Config.IpAddress = &vmmCommonModels.IPv4Address{
		Value: ptr.To(MockIP),
	}

	nicNetInfo.Ipv4Info = vmmModels.NewIpv4Info()
	nicNetInfo.Ipv4Info.LearnedIpAddresses = []vmmCommonModels.IPv4Address{
		{
			Value: ptr.To(MockIP),
		},
	}

	err := nic.SetNicNetworkInfo(*nicNetInfo)
	if err != nil {
		fmt.Printf("error setting nic network info: %+v\n", err)
		return nil
	}

	vm := &vmmModels.Vm{
		ExtId:      ptr.To(vmUUID),
		Categories: make([]vmmModels.CategoryReference, 0),
		PowerState: vmmModels.POWERSTATE_ON.Ref(),
		Name:       ptr.To(vmName),
		Cluster: &vmmModels.ClusterReference{
			ExtId: cluster.ExtId,
		},
		Nics: []vmmModels.Nic{
			*nic,
		},
	}
	if host != nil {
		vm.Host = &vmmModels.HostReference{
			ExtId: host.ExtId,
		}
	}
	return vm
}

func getDefaultVMWithDpOffload(vmName string, vmUUID string, cluster *clusterModels.Cluster, host *clusterModels.Host) *vmmModels.Vm {
	nic := vmmModels.NewNic()
	nicNetInfo := vmmModels.NewDpOffloadNicNetworkInfo()

	nicNetInfo.Ipv4Config = vmmModels.NewIpv4Config()
	nicNetInfo.Ipv4Config.IpAddress = &vmmCommonModels.IPv4Address{
		Value: ptr.To(MockIP),
	}

	nicNetInfo.Ipv4Info = vmmModels.NewIpv4Info()
	nicNetInfo.Ipv4Info.LearnedIpAddresses = []vmmCommonModels.IPv4Address{
		{
			Value: ptr.To(MockIP),
		},
	}

	err := nic.SetNicNetworkInfo(*nicNetInfo)
	if err != nil {
		fmt.Printf("error setting nic network info: %+v\n", err)
		return nil
	}

	vm := &vmmModels.Vm{
		ExtId:      ptr.To(vmUUID),
		Categories: make([]vmmModels.CategoryReference, 0),
		PowerState: vmmModels.POWERSTATE_ON.Ref(),
		Name:       ptr.To(vmName),
		Cluster: &vmmModels.ClusterReference{
			ExtId: cluster.ExtId,
		},
		Nics: []vmmModels.Nic{
			*nic,
		},
	}
	if host != nil {
		vm.Host = &vmmModels.HostReference{
			ExtId: host.ExtId,
		}
	}
	return vm
}

func getDefaultVMWithSecondaryIPs(vmName string, vmUUID string, cluster *clusterModels.Cluster, host *clusterModels.Host) *vmmModels.Vm {
	nic := vmmModels.NewNic()
	nicNetInfo := vmmModels.NewVirtualEthernetNicNetworkInfo()

	nicNetInfo.Ipv4Config = vmmModels.NewIpv4Config()
	nicNetInfo.Ipv4Config.IpAddress = &vmmCommonModels.IPv4Address{
		Value: ptr.To(MockIP),
	}
	// Set secondary IP addresses
	nicNetInfo.Ipv4Config.SecondaryIpAddressList = []vmmCommonModels.IPv4Address{
		{
			Value: ptr.To(MockSecondaryIP1),
		},
		{
			Value: ptr.To(MockSecondaryIP2),
		},
	}

	nicNetInfo.Ipv4Info = vmmModels.NewIpv4Info()
	nicNetInfo.Ipv4Info.LearnedIpAddresses = []vmmCommonModels.IPv4Address{
		{
			Value: ptr.To(MockIP),
		},
	}

	err := nic.SetNicNetworkInfo(*nicNetInfo)
	if err != nil {
		fmt.Printf("error setting nic network info: %+v\n", err)
		return nil
	}

	vm := &vmmModels.Vm{
		ExtId:      ptr.To(vmUUID),
		Categories: make([]vmmModels.CategoryReference, 0),
		PowerState: vmmModels.POWERSTATE_ON.Ref(),
		Name:       ptr.To(vmName),
		Cluster: &vmmModels.ClusterReference{
			ExtId: cluster.ExtId,
		},
		Nics: []vmmModels.Nic{
			*nic,
		},
	}
	if host != nil {
		vm.Host = &vmmModels.HostReference{
			ExtId: host.ExtId,
		}
	}
	return vm
}

func getDefaultCluster(clusterName string, clusterUUID string) *clusterModels.Cluster {
	cluster := clusterModels.NewCluster()
	cluster.ExtId = ptr.To(clusterUUID)
	cluster.Name = ptr.To(clusterName)
	cluster.Config = &clusterModels.ClusterConfigReference{
		ClusterSoftwareMap: make([]clusterModels.SoftwareMapReference, 0),
	}
	return cluster
}

func getDefaultHost(hostName string, hostUUID string, clusterUUID string) *clusterModels.Host {
	host := clusterModels.NewHost()
	host.ExtId = ptr.To(hostUUID)
	host.HostName = ptr.To(hostName)
	host.Cluster = &clusterModels.ClusterReference{
		Uuid: ptr.To(clusterUUID),
	}
	return host
}

func getDefaultCategory(categoryName string, categoryUUID string, categoryValue string) *prismModels.Category {
	category := prismModels.NewCategory()
	category.ExtId = ptr.To(categoryUUID)
	category.Key = ptr.To(categoryName)
	category.Value = ptr.To(categoryValue)
	return category
}

func createNodeForVM(ctx context.Context, kClient *fake.Clientset, vm *vmmModels.Vm) (*v1.Node, error) {
	n := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: *vm.Name,
		},

		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{
				SystemUUID: *vm.ExtId,
			},
		},
	}
	node, err := kClient.CoreV1().Nodes().Create(ctx, n, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return node, nil
}

func ValidateInstanceMetadata(metadata *cloudprovider.InstanceMetadata, vm *vmmModels.Vm, region, zone string) {
	Expect(metadata).NotTo(BeNil())                                               // nolint:typecheck
	Expect(metadata.InstanceType).To(Equal(constants.InstanceType))               // nolint:typecheck
	Expect(metadata.ProviderID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.ExtId))) // nolint:typecheck
	Expect(metadata.Region).To(Equal(region))                                     // nolint:typecheck
	Expect(metadata.Zone).To(Equal(zone))                                         // nolint:typecheck
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

func CheckAdditionalLabels(node *v1.Node, vm *vmmModels.Vm, cluster *clusterModels.Cluster, host *clusterModels.Host) {
	Expect(vm).ToNot(BeNil())   // nolint:typecheck
	Expect(node).ToNot(BeNil()) // nolint:typecheck

	toMatchKeys := gstruct.Keys{
		constants.CustomPEUUIDLabel: Equal(*vm.Cluster.ExtId), // nolint:typecheck
		constants.CustomPENameLabel: Equal(*cluster.Name),     // nolint:typecheck
	}
	if host != nil && host.ExtId != nil && host.HostName != nil {
		toMatchKeys[constants.CustomHostUUIDLabel] = Equal(*host.ExtId)    // nolint:typecheck
		toMatchKeys[constants.CustomHostNameLabel] = Equal(*host.HostName) // nolint:typecheck
	}

	Expect(node.Labels).To(gstruct.MatchAllKeys(toMatchKeys)) // nolint:typecheck
}

func CreatePrismCentralCluster(clusterName string, clusterUUID string) *clusterModels.Cluster {
	pc := getDefaultCluster(clusterName, clusterUUID)
	pc.Config.ClusterSoftwareMap = []clusterModels.SoftwareMapReference{
		{
			SoftwareType: clusterModels.SOFTWARETYPEREF_PRISM_CENTRAL.Ref(),
		},
	}
	return pc
}
