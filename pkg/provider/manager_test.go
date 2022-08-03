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
)

var _ = Describe("Test Manager", func() {
	var (
		ctx             context.Context
		kClient         *fake.Clientset
		mockEnvironment *mock.MockEnvironment
		m               nutanixManager
		err             error
	)

	BeforeEach(func() {
		ctx = context.TODO()
		kClient = fake.NewSimpleClientset()
		mockEnvironment, err = mock.CreateMockEnvironment(ctx, kClient)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mockEnvironment).ToNot(BeNil())
		m = nutanixManager{
			config: config.Config{
				TopologyCategories: &config.TopologyCategories{
					Region: mock.MockDefaultRegion,
					Zone:   mock.MockDefaultZone,
				},
			},
			client:        kClient,
			nutanixClient: mock.CreateMockClient(*mockEnvironment),
		}
	})

	Context("Test HasEmptyTopologyInfo", func() {
		It("should detect emptyTopologyInfo", func() {
			c := config.TopologyCategories{}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty zone", func() {
			c := config.TopologyCategories{
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect empty region", func() {
			c := config.TopologyCategories{
				Zone: mock.MockZone,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeTrue())
		})

		It("should detect non-empty region", func() {
			c := config.TopologyCategories{
				Zone:   mock.MockZone,
				Region: mock.MockRegion,
			}
			Expect(m.hasEmptyTopologyInfo(c)).To(BeFalse())
		})
	})

	Context("Test IsVMShutdown", func() {
		It("should detect if VM is powered off", func() {
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOff)
			Expect(vm).ToNot(BeNil())
			Expect(m.isVMShutdown(vm)).To(BeTrue())
		})

		It("should detect if VM is powered on", func() {
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOn)
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
			vm := mockEnvironment.GetVM(mock.MockVMNameNoAddresses)
			Expect(vm).ToNot(BeNil())
			_, err := m.getNodeAddresses(ctx, vm)
			Expect(err).Should(HaveOccurred())
		})

		It("should fetch the correct node addresses", func() {
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOn)
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
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOn)
			Expect(vm).ToNot(BeNil())
			providerID, err := m.generateProviderID(ctx, *vm.Metadata.UUID)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerID).To(Equal(fmt.Sprintf("nutanix://%s", *vm.Metadata.UUID)))
		})
	})
})
