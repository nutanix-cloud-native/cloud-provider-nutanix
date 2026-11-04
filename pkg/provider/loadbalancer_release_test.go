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

package provider

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/loadbalancer"
)

func TestHeldReservationUsesAutoIPAMOnRelease(t *testing.T) {
	resolver, err := loadbalancer.NewResolver(config.LoadBalancer{DefaultStrategy: loadbalancer.StrategyNone})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	nc := &NtnxCloud{lbResolver: resolver}
	held := &v1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		constants.LoadBalancerIPAnnotation:            "10.1.1.1",
		constants.LoadBalancerReservationIDAnnotation: "res-1",
	}}}

	status, exists, err := nc.GetLoadBalancer(context.Background(), "cluster", held)
	if err != nil || !exists || status == nil || len(status.Ingress) != 1 || status.Ingress[0].IP != "10.1.1.1" {
		t.Fatalf("GetLoadBalancer = (%v, %v, %v), want the held address", status, exists, err)
	}
	if err := nc.EnsureLoadBalancerDeleted(context.Background(), "cluster", held); err == nil {
		t.Fatal("delete of a held reservation returned nil without clients")
	}

	plain := &v1.Service{}
	status, exists, err = nc.GetLoadBalancer(context.Background(), "cluster", plain)
	if err != nil || exists || status != nil {
		t.Fatalf("plain GetLoadBalancer = (%v, %v, %v)", status, exists, err)
	}
	if err := nc.EnsureLoadBalancerDeleted(context.Background(), "cluster", plain); err != nil {
		t.Fatalf("None delete: %v", err)
	}
}
