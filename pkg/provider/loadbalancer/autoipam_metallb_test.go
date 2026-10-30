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

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	cloudprovider "k8s.io/cloud-provider"
	servicehelpers "k8s.io/cloud-provider/service/helpers"
)

func TestAutoIPAMEmptySubnetSkipsReserveAndPool(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	strategy := newAutoStrategy(config.AutoIPAMMetalLB{}, prism.Prism(), kube, dyn)

	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err == nil {
		t.Fatal("expected error for empty subnet, got nil")
	}
	if prism.reserveCalls != 0 {
		t.Fatalf("ReserveIP calls = %d, want 0", prism.reserveCalls)
	}
	if poolExists(t, dyn, defaultMetalLBNamespace, "uid-web") {
		t.Fatal("empty subnet created a pool")
	}
}

func TestAutoIPAMReserveFailureSkipsPool(t *testing.T) {
	ctx := context.Background()
	svc := lbService("default", "web", "uid-web")

	t.Run("prism error", func(t *testing.T) {
		prism := &countingPrism{reserveErr: errors.New("prism unavailable")}
		kube, dyn := newClients(svc.DeepCopy())
		strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
		if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err == nil {
			t.Fatal("expected reserve error, got nil")
		}
		got := mustGetService(t, kube, "default", "web")
		if got.Annotations[constants.LoadBalancerReservationIDAnnotation] == "" {
			t.Fatal("retryable prism error cleared the reservation id")
		}
		assertNoPoolOrMetalLB(t, kube, dyn, got)
	})

	t.Run("pending", func(t *testing.T) {
		prism := &countingPrism{reserveErr: interfaces.ErrIPTaskPending}
		kube, dyn := newClients(svc.DeepCopy())
		strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
		_, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
		if !errors.Is(err, interfaces.ErrIPTaskPending) {
			t.Fatalf("expected pending error, got %v", err)
		}
		got := mustGetService(t, kube, "default", "web")
		if got.Annotations[constants.LoadBalancerReservationIDAnnotation] == "" {
			t.Fatal("pending reserve cleared the reservation id")
		}
		assertNoPoolOrMetalLB(t, kube, dyn, got)
	})

	t.Run("terminal", func(t *testing.T) {
		prism := &countingPrism{reserveErr: interfaces.ErrIPReservationTerminal}
		kube, dyn := newClients(svc.DeepCopy())
		strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
		_, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
		if !errors.Is(err, interfaces.ErrIPReservationTerminal) {
			t.Fatalf("expected terminal error, got %v", err)
		}
		got := mustGetService(t, kube, "default", "web")
		if got.Annotations[constants.LoadBalancerReservationIDAnnotation] == "" {
			t.Fatal("terminal reserve failure cleared the reservation id")
		}
		assertNoPoolOrMetalLB(t, kube, dyn, got)
	})
}

func TestAutoIPAMActivationDoesNotAddCleanupFinalizer(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.0.0.8"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := mustGetService(t, kube, "default", "web")
	for _, f := range got.Finalizers {
		if f == servicehelpers.LoadBalancerCleanupFinalizer {
			t.Fatal("activation added the cleanup finalizer")
		}
	}
}

func TestAutoIPAMReservationOrderAndPoolSpec(t *testing.T) {
	ctx := context.Background()
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	prism := &countingPrism{ip: "10.2.0.5"}
	prism.onReserve = func() {
		got := mustGetService(t, kube, "default", "web")
		if got.Annotations[constants.LoadBalancerReservationIDAnnotation] == "" {
			t.Fatal("reservation id was not written before ReserveIP")
		}
		if got.Annotations[constants.MetalLBRequestedIPsAnnotation] != "" {
			t.Fatal("MetalLB annotation was written before ReserveIP")
		}
		if poolExists(t, dyn, "metallb-system", "uid-web") {
			t.Fatal("pool existed before ReserveIP")
		}
	}
	strategy := newAutoStrategy(config.AutoIPAMMetalLB{Subnet: "subnet-ext"}, prism.Prism(), kube, dyn)
	status, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil || len(status.Ingress) != 1 || status.Ingress[0].IP != "10.2.0.5" {
		t.Fatalf("status = %#v, want IP 10.2.0.5", status)
	}

	got := mustGetService(t, kube, "default", "web")
	if got.Annotations[constants.MetalLBRequestedIPsAnnotation] != "10.2.0.5" {
		t.Fatalf("MetalLB annotation = %q", got.Annotations[constants.MetalLBRequestedIPsAnnotation])
	}
	if got.Annotations[constants.LoadBalancerIPAnnotation] != "10.2.0.5" {
		t.Fatalf("IP annotation = %q", got.Annotations[constants.LoadBalancerIPAnnotation])
	}
	if got.Annotations[constants.LoadBalancerSubnetAnnotation] != "subnet-ext" {
		t.Fatalf("subnet annotation = %q", got.Annotations[constants.LoadBalancerSubnetAnnotation])
	}
	pool := mustGetPool(t, dyn, defaultMetalLBNamespace, "uid-web")
	assertPool(t, pool, "uid-web", got.Annotations[constants.LoadBalancerReservationIDAnnotation], "subnet-ext", "10.2.0.5")
}

func TestAutoIPAMMetalLBAnnotationAfterPool(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.2.0.6"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	dyn.PrependReactor("create", "ipaddresspools", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pool create failed")
	})
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err == nil {
		t.Fatal("expected pool create error, got nil")
	}
	got := mustGetService(t, kube, "default", "web")
	if got.Annotations[constants.LoadBalancerIPAnnotation] != "10.2.0.6" {
		t.Fatalf("IP annotation = %q, want the reserved address persisted before the pool", got.Annotations[constants.LoadBalancerIPAnnotation])
	}
	if got.Annotations[constants.MetalLBRequestedIPsAnnotation] != "" {
		t.Fatal("MetalLB annotation was written although the pool was not created")
	}
}

func TestAutoIPAMSecondEnsureDoesNotReserveAgain(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.2.0.7"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if prism.reserveCalls != 1 {
		t.Fatalf("ReserveIP calls = %d, want 1", prism.reserveCalls)
	}
}

func TestAutoIPAMForeignPoolLeftUnchanged(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.2.0.8"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	foreign := newIPAddressPool("metallb-system", "uid-web", "10.9.9.9", "other-subnet", "other-reservation")
	foreign.Labels[constants.LoadBalancerServiceUIDLabel] = "someone-else"
	delete(foreign.Labels, constants.LoadBalancerIPLabel)
	foreignObj, err := unstructuredFromPool(foreign)
	if err != nil {
		t.Fatalf("encode foreign pool: %v", err)
	}
	if _, err := dyn.Resource(ipAddressPoolGVR).Namespace("metallb-system").Create(ctx, foreignObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create foreign pool: %v", err)
	}
	before := foreign.DeepCopy()
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err == nil {
		t.Fatal("expected identity mismatch error, got nil")
	}
	got := mustGetPool(t, dyn, "metallb-system", "uid-web")
	if got.Labels[constants.LoadBalancerServiceUIDLabel] != before.Labels[constants.LoadBalancerServiceUIDLabel] {
		t.Fatalf("foreign pool labels changed: %#v", got.Labels)
	}
	if got.Labels[constants.LoadBalancerIPLabel] != "" {
		t.Fatal("foreign pool identity was overwritten")
	}
	svc := mustGetService(t, kube, "default", "web")
	if svc.Annotations[constants.MetalLBRequestedIPsAnnotation] != "" {
		t.Fatal("MetalLB annotation set despite pool identity mismatch")
	}
}

func TestAutoIPAMUpdateLoadBalancerImplementedElsewhere(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.2.0.9"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	status, err := strategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	err = strategy.UpdateLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
	if !errors.Is(err, cloudprovider.ImplementedElsewhere) {
		t.Fatalf("UpdateLoadBalancer = %v, want ImplementedElsewhere", err)
	}
	if prism.reserveCalls != 1 {
		t.Fatalf("ReserveIP calls = %d, want 1", prism.reserveCalls)
	}
	got := mustGetService(t, kube, "default", "web")
	if got.Annotations[constants.LoadBalancerIPAnnotation] != status.Ingress[0].IP {
		t.Fatalf("IP annotation = %q, want %q", got.Annotations[constants.LoadBalancerIPAnnotation], status.Ingress[0].IP)
	}
	if !poolExists(t, dyn, "metallb-system", "uid-web") {
		t.Fatal("update removed the pool")
	}
}

func TestAutoIPAMGetLoadBalancer(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{ip: "10.2.0.10"}
	kube, dyn := newClients(lbService("default", "web", "uid-web"))
	strategy := newAutoStrategy(testAutoConfig(), prism.Prism(), kube, dyn)
	before := mustGetService(t, kube, "default", "web")
	status, exists, err := strategy.GetLoadBalancer(ctx, "cluster", before)
	if err != nil || exists || status != nil {
		t.Fatalf("before activation GetLoadBalancer = (%v, %v, %v), want (nil, false, nil)", status, exists, err)
	}
	if _, err := strategy.EnsureLoadBalancer(ctx, "cluster", before, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	after := mustGetService(t, kube, "default", "web")
	status, exists, err = strategy.GetLoadBalancer(ctx, "cluster", after)
	if err != nil || !exists || status == nil || status.Ingress[0].IP != "10.2.0.10" {
		t.Fatalf("after activation GetLoadBalancer = (%v, %v, %v)", status, exists, err)
	}
}

func TestAutoIPAMTwoServicesAndRestart(t *testing.T) {
	ctx := context.Background()
	web := lbService("default", "web", "uid-web")
	api := lbService("default", "api", "uid-api")
	web.Finalizers = []string{servicehelpers.LoadBalancerCleanupFinalizer}
	api.Finalizers = []string{servicehelpers.LoadBalancerCleanupFinalizer}
	kube, dyn := newClients(web, api)

	ips := []string{"10.8.0.1", "10.8.0.2"}
	var reserveCalls int
	prism := &mock.MockPrism{
		ReserveIPOverride: func(context.Context, string, string, string) (string, error) {
			if reserveCalls >= len(ips) {
				t.Fatalf("unexpected ReserveIP call %d", reserveCalls+1)
			}
			ip := ips[reserveCalls]
			reserveCalls++
			return ip, nil
		},
	}

	newStrategy := func() *AutoIPAMMetalLB {
		return newAutoStrategy(testAutoConfig(), prism, kube, dyn)
	}

	first := newStrategy()
	webStatus, err := first.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil)
	if err != nil {
		t.Fatalf("ensure web: %v", err)
	}
	apiStatus, err := first.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "api"), nil)
	if err != nil {
		t.Fatalf("ensure api: %v", err)
	}
	if webStatus.Ingress[0].IP == apiStatus.Ingress[0].IP {
		t.Fatalf("services reserved the same IP %s", webStatus.Ingress[0].IP)
	}

	restarted := newStrategy()
	if _, err := restarted.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "web"), nil); err != nil {
		t.Fatalf("ensure web after restart: %v", err)
	}
	if _, err := restarted.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "api"), nil); err != nil {
		t.Fatalf("ensure api after restart: %v", err)
	}
	if reserveCalls != 2 {
		t.Fatalf("ReserveIP calls = %d, want 2", reserveCalls)
	}

	webGot := mustGetService(t, kube, "default", "web")
	apiGot := mustGetService(t, kube, "default", "api")
	if webGot.Annotations[constants.LoadBalancerIPAnnotation] != "10.8.0.1" || apiGot.Annotations[constants.LoadBalancerIPAnnotation] != "10.8.0.2" {
		t.Fatalf("persisted IPs web=%q api=%q", webGot.Annotations[constants.LoadBalancerIPAnnotation], apiGot.Annotations[constants.LoadBalancerIPAnnotation])
	}
	assertPool(t, mustGetPool(t, dyn, "metallb-system", "uid-web"), "uid-web", webGot.Annotations[constants.LoadBalancerReservationIDAnnotation], "subnet-ext", "10.8.0.1")
	assertPool(t, mustGetPool(t, dyn, "metallb-system", "uid-api"), "uid-api", apiGot.Annotations[constants.LoadBalancerReservationIDAnnotation], "subnet-ext", "10.8.0.2")

	t.Run("pending reserve creates no pool", func(t *testing.T) {
		if _, err := kube.CoreV1().Services("default").Create(ctx, lbService("default", "pending", "uid-pending"), metav1.CreateOptions{}); err != nil {
			t.Fatalf("create pending service: %v", err)
		}
		pendingPrism := &countingPrism{reserveErr: interfaces.ErrIPTaskPending}
		pendingStrategy := newAutoStrategy(testAutoConfig(), pendingPrism.Prism(), kube, dyn)
		_, err := pendingStrategy.EnsureLoadBalancer(ctx, "cluster", mustGetService(t, kube, "default", "pending"), nil)
		if !errors.Is(err, interfaces.ErrIPTaskPending) {
			t.Fatalf("expected pending error, got %v", err)
		}
		if poolExists(t, dyn, "metallb-system", "uid-pending") {
			t.Fatal("pending reserve created a pool")
		}
		if !poolExists(t, dyn, "metallb-system", "uid-api") {
			t.Fatal("pending reserve removed the other service pool")
		}
	})
}

func TestAutoIPAMEnsureDeletedDoesNotRelease(t *testing.T) {
	ctx := context.Background()
	prism := &countingPrism{}
	kube, dyn := newClients(reservedService("default", "web", "uid-web"))
	poolObj, err := unstructuredFromPool(newIPAddressPool("metallb-system", "uid-web", "10.4.0.1", "stored-subnet", "res-1"))
	if err != nil {
		t.Fatalf("encode pool: %v", err)
	}
	if _, err := dyn.Resource(ipAddressPoolGVR).Namespace("metallb-system").Create(ctx, poolObj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	strategy := newAutoStrategy(config.AutoIPAMMetalLB{}, prism.Prism(), kube, dyn)
	err = strategy.EnsureLoadBalancerDeleted(ctx, "cluster", mustGetService(t, kube, "default", "web"))
	if err == nil {
		t.Fatal("EnsureLoadBalancerDeleted returned nil")
	}
	if errors.Is(err, cloudprovider.ImplementedElsewhere) {
		t.Fatal("EnsureLoadBalancerDeleted returned ImplementedElsewhere")
	}
	if prism.reserveCalls != 0 {
		t.Fatalf("reserve=%d, want 0", prism.reserveCalls)
	}
	got := mustGetService(t, kube, "default", "web")
	if got.Annotations[constants.LoadBalancerIPAnnotation] != "10.4.0.1" ||
		got.Annotations[constants.LoadBalancerReservationIDAnnotation] != "res-1" ||
		got.Annotations[constants.MetalLBRequestedIPsAnnotation] != "10.4.0.1" {
		t.Fatalf("EnsureLoadBalancerDeleted changed annotations: %#v", got.Annotations)
	}
	if !hasFinalizer(got, servicehelpers.LoadBalancerCleanupFinalizer) {
		t.Fatal("EnsureLoadBalancerDeleted removed the cleanup finalizer")
	}
	if !poolExists(t, dyn, "metallb-system", "uid-web") {
		t.Fatal("EnsureLoadBalancerDeleted deleted the pool")
	}
}

type countingPrism struct {
	reserveCalls int
	ip           string
	reserveErr   error
	onReserve    func()
}

func (c *countingPrism) Prism() *mock.MockPrism {
	return &mock.MockPrism{
		ReserveIPOverride: func(_ context.Context, _, _, _ string) (string, error) {
			c.reserveCalls++
			if c.onReserve != nil {
				c.onReserve()
			}
			if c.reserveErr != nil {
				return "", c.reserveErr
			}
			return c.ip, nil
		},
	}
}

func testAutoConfig() config.AutoIPAMMetalLB {
	return config.AutoIPAMMetalLB{Subnet: "subnet-ext", MetalLBNamespace: "metallb-system"}
}

func newAutoStrategy(cfg config.AutoIPAMMetalLB, prism interfaces.Prism, kube kubernetes.Interface, dyn dynamic.Interface) *AutoIPAMMetalLB {
	return &AutoIPAMMetalLB{config: cfg, deps: Dependencies{Prism: prism, Kube: kube, Dynamic: dyn}}
}

func newClients(services ...*v1.Service) (kubernetes.Interface, *dynamicfake.FakeDynamicClient) {
	objs := make([]runtime.Object, 0, len(services))
	for _, svc := range services {
		objs = append(objs, svc)
	}
	return fake.NewSimpleClientset(objs...), dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
}

func lbService(namespace, name, uid string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
		},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer},
	}
}

func reservedService(namespace, name, uid string) *v1.Service {
	svc := lbService(namespace, name, uid)
	svc.Finalizers = []string{servicehelpers.LoadBalancerCleanupFinalizer}
	svc.Annotations = map[string]string{
		constants.LoadBalancerReservationIDAnnotation: "res-1",
		constants.LoadBalancerIPAnnotation:            "10.4.0.1",
		constants.LoadBalancerSubnetAnnotation:        "stored-subnet",
		constants.MetalLBRequestedIPsAnnotation:       "10.4.0.1",
	}
	return svc
}

func mustGetService(t *testing.T, kube kubernetes.Interface, namespace, name string) *v1.Service {
	t.Helper()
	svc, err := kube.CoreV1().Services(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service %s/%s: %v", namespace, name, err)
	}
	return svc
}

func mustGetPool(t *testing.T, dyn dynamic.Interface, namespace, name string) *metallbv1beta1.IPAddressPool {
	t.Helper()
	obj, err := dyn.Resource(ipAddressPoolGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pool %s/%s: %v", namespace, name, err)
	}
	pool, err := poolFromUnstructured(obj)
	if err != nil {
		t.Fatalf("decode pool %s/%s: %v", namespace, name, err)
	}
	return pool
}

func poolExists(t *testing.T, dyn dynamic.Interface, namespace, name string) bool {
	t.Helper()
	_, err := dyn.Resource(ipAddressPoolGVR).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false
	}
	if err != nil {
		t.Fatalf("get pool %s/%s: %v", namespace, name, err)
	}
	return true
}

func assertNoPoolOrMetalLB(t *testing.T, kube kubernetes.Interface, dyn dynamic.Interface, svc *v1.Service) {
	t.Helper()
	if svc.Annotations[constants.MetalLBRequestedIPsAnnotation] != "" {
		t.Fatal("MetalLB annotation is set")
	}
	if poolExists(t, dyn, "metallb-system", string(svc.UID)) {
		t.Fatal("pool exists")
	}
	_ = kube
}

func assertPool(t *testing.T, pool *metallbv1beta1.IPAddressPool, uid, reservationID, subnet, ip string) {
	t.Helper()
	if len(pool.Spec.Addresses) != 1 || pool.Spec.Addresses[0] != ip+"/32" {
		t.Fatalf("addresses = %#v, want %s/32", pool.Spec.Addresses, ip)
	}
	if pool.Spec.AutoAssign == nil || *pool.Spec.AutoAssign {
		t.Fatalf("autoAssign = %#v, want false", pool.Spec.AutoAssign)
	}
	labels := pool.Labels
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
