//go:build integration

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

package autoipam

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	servicehelpers "k8s.io/cloud-provider/service/helpers"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
)

func TestAutoIPAMActivateViaServiceController(t *testing.T) {
	h := startHarness(t)
	svc := h.createLoadBalancerService("web")
	got := h.waitForActivation(svc.Namespace, svc.Name)

	ip := got.Annotations[constants.LoadBalancerIPAnnotation]
	if ip == "" {
		t.Fatal("missing load-balancer-ip annotation")
	}
	if got.Annotations[constants.LoadBalancerReservationIDAnnotation] == "" {
		t.Fatal("missing reservation-id annotation")
	}
	if got.Annotations[constants.LoadBalancerSubnetAnnotation] != h.subnetExtID {
		t.Fatalf("subnet annotation = %q, want %q", got.Annotations[constants.LoadBalancerSubnetAnnotation], h.subnetExtID)
	}
	if got.Annotations[constants.MetalLBRequestedIPsAnnotation] != ip {
		t.Fatalf("MetalLB requested IPs = %q, want %q", got.Annotations[constants.MetalLBRequestedIPsAnnotation], ip)
	}
	if got.Status.LoadBalancer.Ingress[0].IP != ip {
		t.Fatalf("status IP = %q, want %q", got.Status.LoadBalancer.Ingress[0].IP, ip)
	}
	if !hasFinalizer(got, servicehelpers.LoadBalancerCleanupFinalizer) {
		t.Fatal("missing cleanup finalizer")
	}

	pool := h.getPool(string(got.UID))
	h.logManifests("assert pool", got)
	assertPool(t, pool, string(got.UID), got.Annotations[constants.LoadBalancerReservationIDAnnotation], h.subnetExtID, ip)
}

func TestAutoIPAMIdempotentEnsure(t *testing.T) {
	h := startHarness(t)
	svc := h.createLoadBalancerService("api")
	first := h.waitForActivation(svc.Namespace, svc.Name)
	firstIP := first.Annotations[constants.LoadBalancerIPAnnotation]
	firstRes := first.Annotations[constants.LoadBalancerReservationIDAnnotation]
	poolsBefore := h.listPools()

	updated := first.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations["integration.nutanix.com/requeue"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.kube.CoreV1().Services(updated.Namespace).Update(h.ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("touch service: %v", err)
	}

	err := wait.PollUntilContextTimeout(h.ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		got, err := h.kube.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return got.Annotations["integration.nutanix.com/requeue"] != "", nil
	})
	if err != nil {
		t.Fatalf("wait for requeue annotation: %v", err)
	}

	// Give the controller a chance to run Ensure again.
	time.Sleep(5 * time.Second)
	second := h.waitForActivation(svc.Namespace, svc.Name)
	if second.Annotations[constants.LoadBalancerIPAnnotation] != firstIP {
		t.Fatalf("IP changed after requeue: %q -> %q", firstIP, second.Annotations[constants.LoadBalancerIPAnnotation])
	}
	if second.Annotations[constants.LoadBalancerReservationIDAnnotation] != firstRes {
		t.Fatalf("reservation id changed after requeue")
	}
	if pools := h.listPools(); pools != poolsBefore {
		t.Fatalf("pool count = %d, want %d", pools, poolsBefore)
	}
	h.logManifests("after idempotent requeue", second)
}

func TestAutoIPAMDeleteStubKeepsReservation(t *testing.T) {
	h := startHarness(t)
	svc := h.createLoadBalancerService("delete-me")
	got := h.waitForActivation(svc.Namespace, svc.Name)
	uid := string(got.UID)
	ip := got.Annotations[constants.LoadBalancerIPAnnotation]

	if err := h.kube.CoreV1().Services(svc.Namespace).Delete(h.ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete service: %v", err)
	}

	err := wait.PollUntilContextTimeout(h.ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		cur, err := h.kube.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return cur.DeletionTimestamp != nil, nil
	})
	if err != nil {
		t.Fatalf("wait for deletionTimestamp: %v", err)
	}

	// EnsureLoadBalancerDeleted is a stub: finalizer and reservation stay.
	time.Sleep(5 * time.Second)
	cur, err := h.kube.CoreV1().Services(svc.Namespace).Get(h.ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deleting service: %v", err)
	}
	if !hasFinalizer(cur, servicehelpers.LoadBalancerCleanupFinalizer) {
		t.Fatal("cleanup finalizer was removed despite delete stub")
	}
	if cur.Annotations[constants.LoadBalancerIPAnnotation] != ip {
		t.Fatalf("IP annotation cleared: %q", cur.Annotations[constants.LoadBalancerIPAnnotation])
	}
	_ = h.getPool(uid)
	h.logManifests("after delete stub (reservation kept)", cur)

	lb, ok := h.cloud.LoadBalancer()
	if !ok {
		t.Fatal("cloud has no LoadBalancer")
	}
	delErr := lb.EnsureLoadBalancerDeleted(h.ctx, integrationCluster, cur)
	if delErr == nil {
		t.Fatal("EnsureLoadBalancerDeleted returned nil")
	}
	if errors.Is(delErr, cloudprovider.ImplementedElsewhere) {
		t.Fatal("EnsureLoadBalancerDeleted returned ImplementedElsewhere")
	}
}

func assertPool(t *testing.T, pool *unstructured.Unstructured, uid, reservationID, subnet, ip string) {
	t.Helper()
	addresses, found, err := unstructured.NestedStringSlice(pool.Object, "spec", "addresses")
	if err != nil || !found || len(addresses) != 1 || addresses[0] != ip+"/32" {
		t.Fatalf("addresses = %#v (found=%v err=%v), want %s/32", addresses, found, err, ip)
	}
	autoAssign, found, err := unstructured.NestedBool(pool.Object, "spec", "autoAssign")
	if err != nil || !found || autoAssign {
		t.Fatalf("autoAssign = (%v, found=%v, err=%v), want false", autoAssign, found, err)
	}
	labels := pool.GetLabels()
	if labels[constants.LoadBalancerServiceUIDLabel] != uid ||
		labels[constants.LoadBalancerReservationIDLabel] != reservationID ||
		labels[constants.LoadBalancerSubnetLabel] != subnet ||
		labels[constants.LoadBalancerIPLabel] != ip ||
		labels[constants.LoadBalancerPoolTypeLabel] != constants.LoadBalancerPoolTypeAutoIPAMMetalLB {
		t.Fatalf("labels = %#v", labels)
	}
}

func hasFinalizer(svc *v1.Service, finalizer string) bool {
	for _, f := range svc.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}
