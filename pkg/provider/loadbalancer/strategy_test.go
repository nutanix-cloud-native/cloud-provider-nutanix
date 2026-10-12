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
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
)

func TestNewResolverDefaults(t *testing.T) {
	r, err := NewResolver("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.DefaultStrategy() != StrategyNone {
		t.Fatalf("empty default should fall back to %q, got %q", StrategyNone, r.DefaultStrategy())
	}
}

func TestNewResolverUnknownDefault(t *testing.T) {
	if _, err := NewResolver("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown default strategy, got nil")
	}
}

func TestResolverForUsesDefaultWhenNoAnnotation(t *testing.T) {
	r, err := NewResolver(StrategyNone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, err := r.For(&v1.Service{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name() != StrategyNone {
		t.Fatalf("expected %q, got %q", StrategyNone, s.Name())
	}
}

func TestResolverForNilService(t *testing.T) {
	r, _ := NewResolver(StrategyNone)
	s, err := r.For(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name() != StrategyNone {
		t.Fatalf("expected %q, got %q", StrategyNone, s.Name())
	}
}

func TestResolverForAnnotationOverride(t *testing.T) {
	r, _ := NewResolver(StrategyNone)

	// Known override selects that strategy.
	svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{constants.LoadBalancerStrategyAnnotation: StrategyNone},
	}}
	s, err := r.For(svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name() != StrategyNone {
		t.Fatalf("expected %q, got %q", StrategyNone, s.Name())
	}

	// Unknown override is an error (no silent fallback).
	svc.Annotations[constants.LoadBalancerStrategyAnnotation] = "bogus"
	if _, err := r.For(svc); err == nil {
		t.Fatal("expected error for unknown override strategy, got nil")
	}
}

func TestNoneStrategyContract(t *testing.T) {
	ctx := context.Background()
	n := None{}
	svc := &v1.Service{}

	if n.Name() != StrategyNone {
		t.Fatalf("expected name %q, got %q", StrategyNone, n.Name())
	}

	status, exists, err := n.GetLoadBalancer(ctx, "cluster", svc)
	if status != nil || exists || err != nil {
		t.Fatalf("GetLoadBalancer should be (nil,false,nil); got (%v,%v,%v)", status, exists, err)
	}

	// Ensure/Update must return ImplementedElsewhere so MetalLB keeps ownership.
	if _, err := n.EnsureLoadBalancer(ctx, "cluster", svc, nil); !errors.Is(err, cloudprovider.ImplementedElsewhere) {
		t.Fatalf("EnsureLoadBalancer should return ImplementedElsewhere, got %v", err)
	}
	if err := n.UpdateLoadBalancer(ctx, "cluster", svc, nil); !errors.Is(err, cloudprovider.ImplementedElsewhere) {
		t.Fatalf("UpdateLoadBalancer should return ImplementedElsewhere, got %v", err)
	}

	// Delete must NOT return ImplementedElsewhere; it is a no-op.
	if err := n.EnsureLoadBalancerDeleted(ctx, "cluster", svc); err != nil {
		t.Fatalf("EnsureLoadBalancerDeleted should be nil, got %v", err)
	}
}
