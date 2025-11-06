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

//nolint:typecheck // Test file uses ginkgo/gomega which typecheck doesn't understand well
package provider

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

var _ = Describe("Test Manager", func() { // nolint:typecheck
	var (
		ctx             context.Context
		kClient         *fake.Clientset
		mockEnvironment *mock.MockEnvironment
		m               nutanixManager
		err             error
		nClient         interfaces.Prism
	)

	BeforeEach(func() { // nolint:typecheck
		ctx = context.TODO()
		kClient = fake.NewSimpleClientset()
		mockEnvironment, err = mock.CreateMockEnvironment(ctx, kClient)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mockEnvironment).ToNot(BeNil())
		nutanixClient := mock.CreateMockClient(*mockEnvironment)
		nClient, err = nutanixClient.Get()
		Expect(err).ToNot(HaveOccurred())
		mgr, err := newNutanixManager(
			config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: config.CategoriesTopologyDiscoveryType,
					TopologyCategories: &config.TopologyCategories{
						RegionCategory: mock.MockDefaultRegion,
						ZoneCategory:   mock.MockDefaultZone,
					},
				},
				IgnoredNodeIPs: []string{"127.100.10.1", "127.200.20.1", "127.200.100.1/24", "127.200.200.1-127.200.200.10"},
			},
		)
		Expect(err).ShouldNot(HaveOccurred())
		mgr.client = kClient
		mgr.nutanixClient = nutanixClient
		m = *mgr
	})

	Context("Test HasEmptyTopologyInfo", func() {
		It("should detect emptyTopologyInfo", func() { // nolint:typecheck
			c := config.TopologyInfo{}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty zone", func() { // nolint:typecheck
			c := config.TopologyInfo{
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty region", func() { // nolint:typecheck
			c := config.TopologyInfo{
				Zone: mock.MockZone,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect non-empty region", func() { // nolint:typecheck
			c := config.TopologyInfo{
				Zone:   mock.MockZone,
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeFalse())
		})
	})

	Context("Test IsVMShutdown", func() {
		It("should detect if VM is powered off", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOff)
			Expect(vm).ToNot(BeNil())
			Expect(m.isVMShutdown(vm)).To(BeTrue())
		})

		It("should detect if VM is powered on", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			Expect(m.isVMShutdown(vm)).To(BeFalse())
		})
	})

	Context("Test GetNodeAddresses", func() {
		It("should fail if nil node is passed", func() { // nolint:typecheck
			_, err := m.getNodeAddresses(ctx, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if nil vm nics is passed", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameNoAddresses)
			Expect(vm).ToNot(BeNil())
			vm.Nics = nil
			_, err := m.getNodeAddresses(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if nil nic network info is passed", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameNoAddresses)
			Expect(vm).ToNot(BeNil())
			vm.Nics = []vmmModels.Nic{
				{
					NicNetworkInfo: nil,
				},
			}
			_, err := m.getNodeAddresses(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if no node addresses are found", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameNoAddresses)
			Expect(vm).ToNot(BeNil())
			_, err := m.getNodeAddresses(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fetch the correct node addresses", func() { // nolint:typecheck
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
							"Address": Equal(*vm.Name),
						},
					),
				),
			)
		})

		It("should filter node addresses if matching specified filtered addresses", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameFilteredNodeAddresses)
			Expect(vm).ToNot(BeNil())
			addresses, err := m.getNodeAddresses(ctx, vm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(addresses)).To(Equal(2), "Received addresses: %v", addresses)
			Expect(addresses).Should(ConsistOf(
				v1.NodeAddress{Type: v1.NodeInternalIP, Address: mock.MockIP},
				v1.NodeAddress{Type: v1.NodeHostName, Address: *vm.Name},
			))
		})

		It("should fetch the correct node addresses from DpOffloadNicNetworkInfo", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameDpOffload)
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
							"Address": Equal(*vm.Name),
						},
					),
				),
			)
		})

		It("should fetch secondary IP addresses from SecondaryIpAddressList", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameSecondaryIPs)
			Expect(vm).ToNot(BeNil())
			addresses, err := m.getNodeAddresses(ctx, vm)
			Expect(err).ShouldNot(HaveOccurred())
			// Should have primary IP, 2 secondary IPs, and hostname = 4 addresses
			Expect(len(addresses)).To(Equal(4))
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
							"Type":    Equal(v1.NodeInternalIP),
							"Address": Equal(mock.MockSecondaryIP1),
						},
					),
					gstruct.MatchFields(
						gstruct.IgnoreExtras,
						gstruct.Fields{
							"Type":    Equal(v1.NodeInternalIP),
							"Address": Equal(mock.MockSecondaryIP2),
						},
					),
					gstruct.MatchFields(
						gstruct.IgnoreExtras,
						gstruct.Fields{
							"Type":    Equal(v1.NodeHostName),
							"Address": Equal(*vm.Name),
						},
					),
				),
			)
		})
	})

	Context("Test generateProviderID", func() {
		It("should fail if vmUUID is empty", func() { // nolint:typecheck
			_, err := m.generateProviderID(ctx, "")
			Expect(err).Should(HaveOccurred())
		})

		It("should return providerID in valid format", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			providerID, err := m.generateProviderID(ctx, *vm.ExtId)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.ExtId)))
		})
	})

	Context("Test getTopologyInfoFromVM", func() {
		It("should fail if vm is empty", func() { // nolint:typecheck
			err := m.getTopologyInfoFromVM(ctx, nClient, nil, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if topologyInfo is empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			err := m.getTopologyInfoFromVM(ctx, nClient, vm, nil)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test getTopologyInfoFromCluster", func() {
		It("should fail if nutanixClient is empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			err := m.getTopologyInfoFromCluster(ctx, nil, vm, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if vm is empty", func() { // nolint:typecheck
			err = m.getTopologyInfoFromCluster(ctx, nClient, nil, &config.TopologyInfo{})
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if topologyInfo is empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(err).ToNot(HaveOccurred())
			err = m.getTopologyInfoFromCluster(ctx, nClient, vm, nil)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Test getTopologyInfoUsingPrism", func() {
		It("should fail if nutanixClient is empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			_, err := m.getTopologyInfoUsingPrism(ctx, nil, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if vm is empty", func() { // nolint:typecheck
			_, err := m.getTopologyInfoUsingPrism(ctx, nClient, nil)
			Expect(err).Should(HaveOccurred())
		})
	})
})
