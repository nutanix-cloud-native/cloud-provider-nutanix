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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
)

var _ = Describe("Test Loadbalancer", func() { // nolint:typecheck
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
