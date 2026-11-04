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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
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

func TestAutoIPAMDeleteReleasesReservation(t *testing.T) {
	h := startHarness(t)
	svc := h.createLoadBalancerService("delete-me")
	got := h.waitForActivation(svc.Namespace, svc.Name)
	uid := string(got.UID)

	if err := h.kube.CoreV1().Services(svc.Namespace).Delete(h.ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete service: %v", err)
	}

	// The Service controller keeps the cleanup finalizer until unreserve and
	// pool deletion succeed, then the API removes the Service.
	err := wait.PollUntilContextTimeout(h.ctx, time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := h.kube.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if err == nil {
			return false, nil
		}
		_, poolErr := h.dyn.Resource(ipAddressPoolGVR).Namespace(h.metalLBNS).Get(ctx, uid, metav1.GetOptions{})
		if apierrors.IsNotFound(poolErr) {
			return true, nil
		}
		if poolErr != nil {
			return false, poolErr
		}
		return false, nil
	})
	if err != nil {
		if cur, getErr := h.kube.CoreV1().Services(svc.Namespace).Get(h.ctx, svc.Name, metav1.GetOptions{}); getErr == nil {
			h.logManifests("delete timed out", cur)
		}
		t.Fatalf("wait for reservation release: %v", err)
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
