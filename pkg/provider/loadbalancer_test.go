package provider

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
)

var _ = Describe("Test Loadbalancer", func() {
	var (
		ctx       context.Context
		ntnxCloud NtnxCloud
	)

	BeforeEach(func() {
		ctx = context.Background()
		ntnxCloud = NtnxCloud{}
	})

	Context("Test GetLoadBalancer", func() {
		It("should return empty outputs", func() {
			lbStatus, found, err := ntnxCloud.GetLoadBalancer(ctx, mock.MockCluster, &v1.Service{})
			Expect(lbStatus).To(BeNil())
			Expect(found).To(BeFalse())
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Test GetLoadBalancerName", func() {
		It("should return empty outputs", func() {
			n := ntnxCloud.GetLoadBalancerName(ctx, mock.MockCluster, &v1.Service{})
			Expect(n).To(BeEmpty())
		})
	})

	Context("Test EnsureLoadBalancer", func() {
		It("should not return error", func() {
			lbStatus, err := ntnxCloud.EnsureLoadBalancer(ctx, mock.MockCluster, &v1.Service{}, []*v1.Node{})
			Expect(lbStatus).To(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Test UpdateLoadBalancer", func() {
		It("should not return error", func() {
			err := ntnxCloud.UpdateLoadBalancer(ctx, mock.MockCluster, &v1.Service{}, []*v1.Node{})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Test UpdateLoadBalancer", func() {
		It("not return error", func() {
			err := ntnxCloud.UpdateLoadBalancer(ctx, mock.MockCluster, &v1.Service{}, []*v1.Node{})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Test EnsureLoadNalancerDeleted", func() {
		It("not return error", func() {
			err := ntnxCloud.EnsureLoadBalancerDeleted(ctx, mock.MockCluster, &v1.Service{})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
