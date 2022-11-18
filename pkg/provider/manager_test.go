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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

var _ = Describe("Test Manager", func() {
	var (
		ctx             context.Context
		kClient         *fake.Clientset
		mockEnvironment *mock.MockEnvironment
		m               nutanixManager
		err             error
		nClient         interfaces.Prism
	)

	BeforeEach(func() {
		ctx = context.TODO()
		kClient = fake.NewSimpleClientset()
		mockEnvironment, err = mock.CreateMockEnvironment(ctx, kClient)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mockEnvironment).ToNot(BeNil())
		nutanixClient := mock.CreateMockClient(*mockEnvironment)
		nClient, err = nutanixClient.Get()
		Expect(err).ToNot(HaveOccurred())
		m = nutanixManager{
			config: config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: config.CategoriesTopologyDiscoveryType,
					TopologyCategories: &config.TopologyCategories{
						RegionCategory: mock.MockDefaultRegion,
						ZoneCategory:   mock.MockDefaultZone,
					},
				},
			},
			client:        kClient,
			nutanixClient: nutanixClient,
		}
	})

	Context("Test HasEmptyTopologyInfo", func() {
		It("should detect emptyTopologyInfo", func() {
			c := config.TopologyInfo{}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty zone", func() {
			c := config.TopologyInfo{
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty region", func() {
			c := config.TopologyInfo{
				Zone: mock.MockZone,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect non-empty region", func() {
			c := config.TopologyInfo{
				Zone:   mock.MockZone,
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeFalse())
		})
	})

	Context("Test IsVMShutdown", func() {
		It("should detect if VM is powered off", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOff)
			Expect(vm).ToNot(BeNil())
			Expect(m.isVMShutdown(vm)).To(BeTrue())
		})

		It("should detect if VM is powered on", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			Expect(m.isVMShutdown(vm)).To(BeFalse())
		})
	})

	Context("Test GetNodeAddresses", func() {
		It("should fail if nil node is passed", func() {
			_, err := m.getNodeAddresses(ctx, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if no node addresses are found", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameNoAddresses)
			Expect(vm).ToNot(BeNil())
			_, err := m.getNodeAddresses(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fetch the correct node addresses", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			addresses, err := m.getNodeAddresses(ctx, vm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(addresses)).To(Equal(2))
			Expect(addresses).Should(
				ContainElements(
					gstruct.MatchFields(
						gstruct.IgnoreExtras,
						gstruct.Fields{
							"Type":    Equal(v1.NodeInternalIP),
							"Address": Equal(mock.MockIP),
						},
					),
					gstruct.MatchFields(
						gstruct.IgnoreExtras,
						gstruct.Fields{
							"Type":    Equal(v1.NodeHostName),
							"Address": Equal(*vm.Spec.Name),
						},
					),
				),
			)
		})
	})

	Context("Test generateProviderID", func() {
		It("should fail if vmUUID is empty", func() {
			_, err := m.generateProviderID(ctx, "")
			Expect(err).Should(HaveOccurred())
		})

		It("should return providerID in valid format", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			providerID, err := m.generateProviderID(ctx, *vm.Metadata.UUID)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.Metadata.UUID)))
		})
	})

	Context("Test getTopologyInfoFromVM", func() {
		It("should fail if vm is empty", func() {
			err := m.getTopologyInfoFromVM(nil, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if topologyInfo is empty", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			err := m.getTopologyInfoFromVM(vm, nil)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test getTopologyInfoFromCluster", func() {
		It("should fail if nutanixClient is empty", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			err := m.getTopologyInfoFromCluster(ctx, nil, vm, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if vm is empty", func() {
			err = m.getTopologyInfoFromCluster(ctx, nClient, nil, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if topologyInfo is empty", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(err).ToNot(HaveOccurred())
			err = m.getTopologyInfoFromCluster(ctx, nClient, vm, nil)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test getTopologyInfoUsingPrism", func() {
		It("should fail if nutanixClient is empty", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			_, err := m.getTopologyInfoUsingPrism(ctx, nil, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if vm is empty", func() {
			_, err := m.getTopologyInfoUsingPrism(ctx, nClient, nil)
			Expect(err).Should(HaveOccurred())
		})
	})
})
