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
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/prism-go-client/converged"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
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

	Context("Test generateProviderIDFromVM", func() {
		It("should fail if VM is nil", func() { // nolint:typecheck
			_, err := m.generateProviderIDFromVM(ctx, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should return providerID from BiosUuid when no customAttributes", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.BiosUuid)))
		})

		It("should prefer BiosUuid over ExtId", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.BiosUuid = ptr.To("bios-uuid-different")
			vm.ExtId = ptr.To("ext-id-different")
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal("nutanix://bios-uuid-different"))
		})

		It("should fallback to ExtId when BiosUuid is nil", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.BiosUuid = nil
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.ExtId)))
		})

		It("should fail when both BiosUuid and ExtId are empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.BiosUuid = nil
			vm.ExtId = nil
			vm.CustomAttributes = nil
			_, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should return providerID from customAttributes when present", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNameCustomProviderID)
			Expect(vm).ToNot(BeNil())
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", mock.MockCustomProviderID)))
		})

		It("should handle customAttributes with spaces around providerID", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			// Add customAttributes with spaces
			vm.CustomAttributes = []string{" providerID : test-uuid-with-spaces ", "otherKey:value"}
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal("nutanix://test-uuid-with-spaces"))
		})

		It("should fallback to ExtId when customAttributes has no providerID", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			// Add customAttributes without providerID
			vm.CustomAttributes = []string{"someKey:someValue", "anotherKey:anotherValue"}
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.ExtId)))
		})

		It("should fallback to ExtId when providerID value is empty", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			// Add customAttributes with empty providerID value
			vm.CustomAttributes = []string{"providerID:", "otherKey:value"}
			providerID, err := m.generateProviderIDFromVM(ctx, vm)
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
			err := m.getTopologyInfoUsingPrism(ctx, nil, vm, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should fail if vm is empty", func() { // nolint:typecheck
			err := m.getTopologyInfoUsingPrism(ctx, nClient, nil, nil)
			Expect(err).Should(HaveOccurred())
		})

		It("should use domain manager and resource groups for project-scoped topology", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To("project-1")}
			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.IsProjectScopedOverride = func(_ context.Context) bool {
				return true
			}
			mockPrism.ListAllClusterOverride = func(_ context.Context) ([]clusterModels.Cluster, error) {
				return nil, fmt.Errorf("ListAllCluster must not be called for project-scoped prism topology")
			}
			mockPrism.GetClusterOverride = func(_ context.Context, _ string) (*clusterModels.Cluster, error) {
				return nil, fmt.Errorf("GetCluster must not be called for project-scoped prism topology")
			}

			topologyInfo := &config.TopologyInfo{}
			err := m.getTopologyInfoUsingPrism(ctx, nClient, vm, topologyInfo)
			Expect(err).ToNot(HaveOccurred())
			Expect(topologyInfo.Region).To(Equal(mock.MockPrismCentral))
			Expect(topologyInfo.Zone).To(Equal(mock.MockClusterUUID))
		})
	})

	Context("Test resolveVM", func() {
		It("should return VM when found by BIOS UUID", func() {
			vm, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vm).ToNot(BeNil())
			Expect(*vm.ExtId).To(Equal(mock.MockVMPoweredOnUUID))
		})

		It("should return not-found error when VM not found by either method", func() {
			_, err := m.resolveVM(ctx, nClient, "non-existing-uuid")
			Expect(err).Should(HaveOccurred())
		})

		It("should fallback to GetVM when BIOS UUID lookup returns not-found", func() {
			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetVMByBiosUUidOverride = func(_ context.Context, _ string) (*vmmModels.Vm, error) {
				return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("BIOS_UUID_NOT_FOUND")}
			}

			vm, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vm).ToNot(BeNil())
			Expect(*vm.ExtId).To(Equal(mock.MockVMPoweredOnUUID))
		})

		It("should fallback to GetVM when BIOS UUID lookup fails with non-not-found error (PC 7.5 compat)", func() {
			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetVMByBiosUUidOverride = func(_ context.Context, _ string) (*vmmModels.Vm, error) {
				return nil, fmt.Errorf("internal server error")
			}

			vm, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(vm).ToNot(BeNil())
			Expect(*vm.ExtId).To(Equal(mock.MockVMPoweredOnUUID))
		})

		Context("on Prism Central 7.6+ (BIOS UUID lookup supported)", func() {
			BeforeEach(func() {
				mockPrism := nClient.(*mock.MockPrism)
				mockPrism.GetPrismCentralVersionOverride = func(_ context.Context) (string, error) {
					return "7.6", nil
				}
			})

			It("should resolve by BIOS UUID without falling back", func() {
				vm, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(vm).ToNot(BeNil())
				Expect(*vm.ExtId).To(Equal(mock.MockVMPoweredOnUUID))
			})

			It("should fall back to GetVM by ExtId on non-not-found errors", func() {
				mockPrism := nClient.(*mock.MockPrism)
				mockPrism.GetVMByBiosUUidOverride = func(_ context.Context, _ string) (*vmmModels.Vm, error) {
					return nil, &converged.APIError{Kind: converged.ErrInternal, Cause: fmt.Errorf("transient")}
				}

				// On a non-not-found error the BIOS UUID lookup is inconclusive,
				// so resolveVM falls back to the ExtId lookup which succeeds.
				vm, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(vm).ToNot(BeNil())
				Expect(*vm.ExtId).To(Equal(mock.MockVMPoweredOnUUID))
			})

			It("should return not-found without falling back to GetVM", func() {
				mockPrism := nClient.(*mock.MockPrism)
				mockPrism.GetVMByBiosUUidOverride = func(_ context.Context, _ string) (*vmmModels.Vm, error) {
					return nil, &converged.APIError{Kind: converged.ErrNotFound, Cause: fmt.Errorf("BIOS_UUID_NOT_FOUND")}
				}

				// A definitive not-found means the VM does not exist, so the
				// error is surfaced instead of being masked by the ExtId fallback.
				_, err := m.resolveVM(ctx, nClient, mock.MockVMPoweredOnUUID)
				Expect(err).Should(HaveOccurred())
				Expect(converged.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	Context("Test Label Functions", func() {
		BeforeEach(func() {
			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetPrismCentralVersionOverride = func(_ context.Context) (string, error) {
				return "7.6", nil
			}
		})

		It("should build labels for project scoped VMs", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To("project-1")}

			labels, err := ProjectScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.ProjectUUIDLabel, "project-1"))
			Expect(labels).To(HaveKeyWithValue(constants.ResourceGroupUUIDLabel, "rg-1"))
			Expect(labels).To(HaveKeyWithValue(constants.PEUUIDLabel, mock.MockClusterUUID))
			Expect(labels).To(HaveKeyWithValue(constants.PENameLabel, mock.MockCluster))
		})

		It("should build labels for non project scoped VMs", func() { // nolint:typecheck
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To("project-1")}

			labels, err := ProjectNonScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.ProjectUUIDLabel, "project-1"))
			Expect(labels).To(HaveKeyWithValue(constants.ResourceGroupUUIDLabel, "rg-1"))
			Expect(labels).To(HaveKeyWithValue(constants.PEUUIDLabel, mock.MockClusterUUID))
			Expect(labels).To(HaveKey(constants.PENameLabel))
			Expect(labels).ToNot(HaveKey(constants.HostUUIDLabel))
			Expect(labels).ToNot(HaveKey(constants.HostNameLabel))
		})

		It("should build host labels for a VM's cluster host", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())

			labels, err := hostLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.HostUUIDLabel, mock.MockHostUUID))
			Expect(labels).To(HaveKey(constants.HostNameLabel))
		})

		It("should keep VM project label when default project is available", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To("project-1")}

			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetDefaultProjectExtIdOverride = func(_ context.Context) *string {
				return ptr.To(zeroUUID)
			}

			labels, err := ProjectNonScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.ProjectUUIDLabel, "project-1"))
			Expect(labels).To(HaveKeyWithValue(constants.ResourceGroupUUIDLabel, "rg-1"))
		})

		It("should use zeroUUID project label when VM has no project", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = nil

			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetDefaultProjectExtIdOverride = func(_ context.Context) *string {
				return ptr.To(zeroUUID)
			}

			labels, err := ProjectNonScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.ProjectUUIDLabel, zeroUUID))
			Expect(labels).ToNot(HaveKey(constants.ResourceGroupUUIDLabel))
		})

		It("should skip resource-group APIs when the VM is in the default project", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			defaultProjectExtId := "default-project-uuid"
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To(defaultProjectExtId)}

			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetDefaultProjectExtIdOverride = func(_ context.Context) *string {
				return ptr.To(defaultProjectExtId)
			}
			mockPrism.GetResourceGroupsOverride = func(_ context.Context) ([]multidomainModels.ResourceGroup, error) {
				Fail("GetResourceGroups must not be called for VMs in the default project")
				return nil, nil
			}

			labels, err := ProjectNonScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(HaveKeyWithValue(constants.ProjectUUIDLabel, defaultProjectExtId))
			Expect(labels).ToNot(HaveKey(constants.ResourceGroupUUIDLabel))
			Expect(labels).To(HaveKeyWithValue(constants.PEUUIDLabel, mock.MockClusterUUID))
		})

		It("should skip project and resource-group labels on PC versions below 7.6", func() {
			vm := mockEnvironment.GetVM(ctx, mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			vm.Project = &vmmModels.ProjectReference{ExtId: ptr.To("project-1")}

			mockPrism := nClient.(*mock.MockPrism)
			mockPrism.GetPrismCentralVersionOverride = func(_ context.Context) (string, error) {
				return "7.5", nil
			}
			mockPrism.GetResourceGroupsOverride = func(_ context.Context) ([]multidomainModels.ResourceGroup, error) {
				Fail("GetResourceGroups must not be called for PC versions below 7.6")
				return nil, nil
			}

			labels, err := ProjectNonScopedLabels(ctx, nClient, vm)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).ToNot(HaveKey(constants.ProjectUUIDLabel))
			Expect(labels).ToNot(HaveKey(constants.ResourceGroupUUIDLabel))
		})
	})

	Context("Test NodeExists", func() {
		It("should return false, nil when VM is not found (NotFound)", func() { // nolint:typecheck
			node := mockEnvironment.GetNode(mock.MockNodeNameVMNotExisting)
			Expect(node).ToNot(BeNil())
			exists, err := m.nodeExists(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("should return true, nil when VM exists", func() { // nolint:typecheck
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			Expect(node).ToNot(BeNil())
			exists, err := m.nodeExists(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
	})
})
