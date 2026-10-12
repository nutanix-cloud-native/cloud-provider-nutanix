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

	v1 "k8s.io/api/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/loadbalancer"
)

// strategyFor resolves the LoadBalancer strategy for a Service: the strategy
// named by the Service override annotation if present, otherwise the configured
// default. It tolerates a nil resolver (e.g. a zero-value NtnxCloud in tests) by
// falling back to the built-in default resolver (None).
func (nc *NtnxCloud) strategyFor(service *v1.Service) (loadbalancer.Strategy, error) {
	resolver := nc.lbResolver
	if resolver == nil {
		var err error
		if resolver, err = loadbalancer.NewResolver(loadbalancer.StrategyNone); err != nil {
			return nil, err
		}
	}
	return resolver.For(service)
}

// GetLoadBalancer returns the status of the load balancer for the Service, and
// whether a CCM-owned load balancer exists for it, by delegating to the
// resolved strategy. The *v1.Service parameter is treated as read-only.
func (nc *NtnxCloud) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (
	*v1.LoadBalancerStatus, bool, error,
) {
	strategy, err := nc.strategyFor(service)
	if err != nil {
		return nil, false, err
	}
	return strategy.GetLoadBalancer(ctx, clusterName, service)
}

// GetLoadBalancerName returns the name of the load balancer. The *v1.Service
// parameter is treated as read-only.
func (nc *NtnxCloud) GetLoadBalancerName(ct context.Context, clusterName string, service *v1.Service) string {
	return ""
}

// EnsureLoadBalancer creates or updates the load balancer for the Service by
// delegating to the resolved strategy. The *v1.Service and *v1.Node parameters
// are treated as read-only.
func (nc *NtnxCloud) EnsureLoadBalancer(ctx context.Context,
	clusterName string, service *v1.Service, nodes []*v1.Node) (
	*v1.LoadBalancerStatus, error,
) {
	strategy, err := nc.strategyFor(service)
	if err != nil {
		return nil, err
	}
	return strategy.EnsureLoadBalancer(ctx, clusterName, service, nodes)
}

// UpdateLoadBalancer updates the backends of the Service load balancer by
// delegating to the resolved strategy.
func (nc *NtnxCloud) UpdateLoadBalancer(ctx context.Context,
	clusterName string, service *v1.Service, nodes []*v1.Node,
) error {
	strategy, err := nc.strategyFor(service)
	if err != nil {
		return err
	}
	return strategy.UpdateLoadBalancer(ctx, clusterName, service, nodes)
}

// EnsureLoadBalancerDeleted deletes the Service load balancer by delegating to
// the resolved strategy.
func (nc *NtnxCloud) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string,
	service *v1.Service,
) error {
	strategy, err := nc.strategyFor(service)
	if err != nil {
		return err
	}
	return strategy.EnsureLoadBalancerDeleted(ctx, clusterName, service)
}
