/*
Copyright 2026 Nutanix, Inc

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

package loadbalancer

import (
	"context"

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
)

// None is the do-nothing strategy. CCM claims no ownership of the Service VIP:
// allocation, status, and advertisement stay with an external controller
// (MetalLB using CAREN-managed static addressRanges). This preserves the legacy
// behaviour that shipped before the Service controller was enabled.
//
// Per the cloud-provider contract (k8s.io/cloud-provider cloud.go):
//   - Ensure/UpdateLoadBalancer return ImplementedElsewhere, so the Service
//     controller does not patch status and leaves the Service to MetalLB.
//   - EnsureLoadBalancerDeleted returns nil (it must not return
//     ImplementedElsewhere), a safe no-op since this strategy allocates nothing.
//   - GetLoadBalancer reports exists=false because this strategy never creates
//     an LB of its own.
//
// Note: enabling the Service controller means the controller itself adds and
// removes the service.kubernetes.io/load-balancer-cleanup finalizer regardless
// of ImplementedElsewhere; that finalizer is the only observable delta versus
// the controller being disabled.
type None struct{}

var _ Strategy = None{}

// Name returns the strategy name.
func (None) Name() string { return StrategyNone }

// GetLoadBalancer reports that no CCM-owned load balancer exists for the Service.
func (None) GetLoadBalancer(_ context.Context, _ string, _ *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	return nil, false, nil
}

// EnsureLoadBalancer delegates to the external backend by returning
// ImplementedElsewhere; the Service controller then skips status handling.
func (None) EnsureLoadBalancer(_ context.Context, _ string, _ *v1.Service, _ []*v1.Node) (*v1.LoadBalancerStatus, error) {
	return nil, cloudprovider.ImplementedElsewhere
}

// UpdateLoadBalancer delegates to the external backend by returning
// ImplementedElsewhere.
func (None) UpdateLoadBalancer(_ context.Context, _ string, _ *v1.Service, _ []*v1.Node) error {
	return cloudprovider.ImplementedElsewhere
}

// EnsureLoadBalancerDeleted is a no-op: this strategy owns no resources. It
// returns nil (never ImplementedElsewhere) so the Service controller completes
// teardown and removes the cleanup finalizer.
func (None) EnsureLoadBalancerDeleted(_ context.Context, _ string, _ *v1.Service) error {
	return nil
}
