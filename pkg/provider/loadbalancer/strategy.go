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

// Package loadbalancer contains the CCM Service LoadBalancer strategies and the
// resolver that selects a strategy per Service. Strategies encapsulate all
// backend-specific behaviour so it does not leak into the main Service
// reconciler (cloudprovider.LoadBalancer implementation on NtnxCloud).
package loadbalancer

import (
	"context"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
)

// StrategyNone is the name of the do-nothing strategy. It delegates the whole
// Service LoadBalancer lifecycle to an external controller (MetalLB), matching
// the legacy static-pool behaviour.
const StrategyNone = "None"

// Strategy encapsulates the backend-specific LoadBalancer operations for a
// Service. The method set mirrors cloudprovider.LoadBalancer minus
// GetLoadBalancerName, which is backend independent and handled by the caller.
//
// Implementations must honour the cloud-provider contract:
//   - When the backend is implemented by a separate controller, Ensure and
//     Update must return cloudprovider.ImplementedElsewhere.
//   - EnsureLoadBalancerDeleted must NOT return ImplementedElsewhere.
//   - GetLoadBalancer must report exists=true only for an LB this strategy
//     created; otherwise exists=false with a nil error.
type Strategy interface {
	// Name returns the registered strategy name.
	Name() string

	GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error)
	EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error)
	UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error
	EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error
}

// Resolver selects the Strategy for a Service: the strategy named by the
// Service override annotation if present, otherwise the configured default.
type Resolver struct {
	registry        map[string]Strategy
	defaultStrategy string
}

// NewResolver builds a Resolver with the built-in strategies registered and the
// given default. An empty defaultStrategy falls back to StrategyNone. It errors
// if defaultStrategy is not a registered strategy.
func NewResolver(defaultStrategy string) (*Resolver, error) {
	registry := map[string]Strategy{
		StrategyNone: None{},
	}

	if defaultStrategy == "" {
		defaultStrategy = StrategyNone
	}
	if _, ok := registry[defaultStrategy]; !ok {
		return nil, fmt.Errorf("unknown default load balancer strategy %q (registered: %v)", defaultStrategy, registeredNames(registry))
	}

	return &Resolver{registry: registry, defaultStrategy: defaultStrategy}, nil
}

// DefaultStrategy returns the resolver's configured default strategy name.
func (r *Resolver) DefaultStrategy() string { return r.defaultStrategy }

// For returns the Strategy for the given Service. The Service override
// annotation (constants.LoadBalancerStrategyAnnotation) takes precedence over
// the configured default. An override that names an unregistered strategy is an
// error, surfaced to the Service reconciler rather than silently falling back.
func (r *Resolver) For(service *v1.Service) (Strategy, error) {
	name := r.defaultStrategy
	if service != nil {
		if v, ok := service.Annotations[constants.LoadBalancerStrategyAnnotation]; ok && v != "" {
			name = v
		}
	}

	strategy, ok := r.registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown load balancer strategy %q (registered: %v)", name, registeredNames(r.registry))
	}
	return strategy, nil
}

func registeredNames(registry map[string]Strategy) []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
