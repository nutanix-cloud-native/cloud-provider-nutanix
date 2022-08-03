package provider

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

var _ = Describe("Test InstancesV2", func() {
	var (
		ctx             context.Context
		kClient         *fake.Clientset
		mockEnvironment *mock.MockEnvironment
		i               instancesV2
		err             error
	)

	BeforeEach(func() {
		ctx = context.TODO()
		kClient = fake.NewSimpleClientset()
		mockEnvironment, err = mock.CreateMockEnvironment(ctx, kClient)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(mockEnvironment).ToNot(BeNil())

		i = instancesV2{
			nutanixManager: &nutanixManager{
				config: config.Config{
					TopologyCategories: &config.TopologyCategories{
						Region: mock.MockDefaultRegion,
						Zone:   mock.MockDefaultZone,
					},
				},
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

		It("should have zone and region set if TopologyCategories passed in config and VM has categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNameCategories)
			vm := mockEnvironment.GetVM(mock.MockVMNameCategories)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, mock.MockRegion, mock.MockZone)
		})

		It("should have zone and region set if TopologyCategories is passed in config and cluster has categories", func() {
			// VM does not have categories but cluster has
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOnClusterCategories)
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOnClusterCategories)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, mock.MockRegion, mock.MockZone)
		})

		It("should not have zone and region set if TopologyCategories is not passed in config and VM has categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNameCategories)
			vm := mockEnvironment.GetVM(mock.MockVMNameCategories)
			// clear config so  topology categories are not configured
			i.nutanixManager.config = config.Config{}
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("should not have zone and region set if TopologyCategories is not passed in config and cluster has categories", func() {
			// VM does not have categories but cluster has
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOnClusterCategories)
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOnClusterCategories)
			// clear config so  topology categories are not configured
			i.nutanixManager.config = config.Config{}
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("should not have zone and region set if TopologyCategories is set and VM does not have categories", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOn)
			metadata, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			mock.ValidateInstanceMetadata(metadata, vm, "", "")
		})

		It("should have all custom labels set if VM is poweredOn", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOn)
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOn)
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			updatedNode, err := kClient.CoreV1().Nodes().Get(ctx, node.ObjectMeta.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			mock.CheckAdditionalLabels(updatedNode, vm)
		})

		It("should not have Prism Host labels set if VM is poweredOff", func() {
			node := mockEnvironment.GetNode(mock.MockVMNamePoweredOff)
			vm := mockEnvironment.GetVM(mock.MockVMNamePoweredOff)
			_, err := i.InstanceMetadata(ctx, node)
			Expect(err).ShouldNot(HaveOccurred())
			updatedNode, err := kClient.CoreV1().Nodes().Get(ctx, node.ObjectMeta.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			mock.CheckAdditionalLabels(updatedNode, vm)
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
