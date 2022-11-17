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

package provider

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
)

var _ = Describe("Test InstancesV2", func() {
	var (
		ctx                    context.Context
		kClient                *fake.Clientset
		mockEnvironment        *mock.MockEnvironment
		i                      instancesV2
		err                    error
		prismTopologyConfig    config.Config
		categoryTopologyConfig config.Config
		additionalPC           *prismClientV3.ClusterIntentResponse
	)

	BeforeEach(func() {
		ctx = context.TODO()
		kClient = fake.NewSimpleClientset()
		mockEnvironment, err = mock.CreateMockEnvironment(ctx, kClient)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mockEnvironment).ToNot(BeNil())
		additionalPC = mock.CreatePrismCentralCluster(rand.String(10))
		prismTopologyConfig = config.Config{
			TopologyDiscovery: config.TopologyDiscovery{
				Type: config.PrismTopologyDiscoveryType,
			},
			EnableCustomLabeling: true,
		}
		categoryTopologyConfig = config.Config{
			TopologyDiscovery: config.TopologyDiscovery{
				Type: config.CategoriesTopologyDiscoveryType,
				TopologyCategories: &config.TopologyCategories{
					RegionCategory: mock.MockDefaultRegion,
					ZoneCategory:   mock.MockDefaultZone,
				},
			},
			EnableCustomLabeling: true,
		}
		i = instancesV2{
			nutanixManager: &nutanixManager{
				config:        categoryTopologyConfig,
				client:        kClient,
				nutanixClient: mock.CreateMockClient(*mockEnvironment),
			},
		}
	})

	Context("Test InstanceExists", func() {
		It("should fail no VM exists for node", func() {
			node := mockEnvironment.GetNode(mock.MockNodeNameVMNotExisting)
			Expect(node).ToNot(BeNil())
			e, err := i.InstanceExists(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeFalse())
		})

		It("should return true if vm exists for node", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			Expect(node).ToNot(BeNil())
			e, err := i.InstanceExists(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeTrue())
		})

		It("should error when system UUID is not set for node", func() {
			node := mockEnvironment.GetNode(mock.MockNodeNameNoSystemUUID)
			Expect(node).ToNot(BeNil())
			_, err := i.InstanceExists(ctx, node)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test InstanceShutdown", func() {
		It("should detect if VM is poweredOff", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOff)
			Expect(node).ToNot(BeNil())
			s, err := i.InstanceShutdown(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(s).To(BeTrue())
		})

		It("should detect if vm is powered on", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			Expect(node).ToNot(BeNil())
			s, err := i.InstanceShutdown(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(s).To(BeFalse())
		})

		It("should error when system UUID is not set for node", func() {
			node := mockEnvironment.GetNode(mock.MockNodeNameNoSystemUUID)
			Expect(node).ToNot(BeNil())
			_, err := i.InstanceShutdown(ctx, node)
			Expect(err).Should(HaveOccurred())
		})

		It("should error when node is nil", func() {
			_, err := i.InstanceShutdown(ctx, nil)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test InstanceV2Metadata", func() {
		It("should fail nil node is passed", func() {
			_, err = i.InstanceMetadata(ctx, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail node without system uuid is passed", func() {
			node := mockEnvironment.GetNode(mock.MockNodeNameNoSystemUUID)
			_, err = i.InstanceMetadata(ctx, node)
			Expect(err).Should(HaveOccurred())
		})

		It("[TopologyDiscovery: Categories] should have zone and region set if TopologyCategories passed in config and VM has categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNameCategories)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameCategories)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, mock.MockRegion, mock.MockZone)
		})

		It("[TopologyDiscovery: Categories] should have zone and region set if TopologyCategories is passed in config and cluster has categories", func() {
			// VM does not have categories but cluster has
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOnClusterCategories)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOnClusterCategories)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, mock.MockRegion, mock.MockZone)
		})

		It("[TopologyDiscovery: Categories] should not have zone and region set if TopologyCategories is not passed in config and VM has categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNameCategories)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameCategories)
			// clear config so  topology categories are not configured
			i.nutanixManager.config.TopologyDiscovery.TopologyCategories = &config.TopologyCategories{}
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("[TopologyDiscovery: Categories] should not have zone and region set if TopologyCategories is not passed in config and cluster has categories", func() {
			// VM does not have categories but cluster has
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOnClusterCategories)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOnClusterCategories)
			// clear config so  topology categories are not configured
			i.nutanixManager.config.TopologyDiscovery.TopologyCategories = &config.TopologyCategories{}
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("[TopologyDiscovery: Categories] should not have zone and region set if TopologyCategories is set and VM does not have categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("[TopologyDiscovery: Prism] should have PC name set as region and PE as zone", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			// Change config to Prism topology discovery
			i.nutanixManager.config = prismTopologyConfig
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, mock.MockPrismCentral, *vm.Status.ClusterReference.Name)
		})

		It("[TopologyDiscovery: Prism] should fail if multiple PCs are found", func() {
			mockEnvironment.AddCluster(additionalPC)
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			// Change config to Prism topology discovery
			i.nutanixManager.config = prismTopologyConfig
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).Should(HaveOccurred())
		})

		It("[TopologyDiscovery: Prism] should fail if no PC is found", func() {
			pc := mockEnvironment.GetCluster(ctx, mock.MockPrismCentral)
			mockEnvironment.DeleteCluster(*pc.Metadata.UUID)
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			// Change config to Prism topology discovery
			i.nutanixManager.config = prismTopologyConfig
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).Should(HaveOccurred())
		})

		It("should have all custom labels set if custom labels are enabled and VM is poweredOn", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			updatedNode, err := kClient.CoreV1().Nodes().Get(ctx, node.ObjectMeta.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			mock.CheckAdditionalLabels(updatedNode, vm)
		})

		It("should not have Prism Host labels set if custom labels are enabled and VM is poweredOff", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOff)
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOff)
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			updatedNode, err := kClient.CoreV1().Nodes().Get(ctx, node.ObjectMeta.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			mock.CheckAdditionalLabels(updatedNode, vm)
		})

		It("should not have any custom labels set if disabled", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			// Change config to disable custom labels
			i.nutanixManager.config.EnableCustomLabeling = false
			_, err = i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			updatedNode, err := kClient.CoreV1().Nodes().Get(ctx, node.ObjectMeta.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedNode.Labels).To(BeEmpty())
		})
	})

	Context("Test NewInstancesV2", func() {
		It("should return non-nil instances", func() {
			manager := &nutanixManager{}
			instances := newInstancesV2(manager)
			Expect(instances).ToNot(BeNil())
		})
	})
})
