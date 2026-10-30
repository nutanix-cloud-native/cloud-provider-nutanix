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
	"fmt"

	"github.com/google/uuid"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

const defaultMetalLBNamespace = "metallb-system"

var ipAddressPoolGVR = schema.GroupVersionResource{
	Group:    "metallb.io",
	Version:  "v1beta1",
	Resource: "ipaddresspools",
}

// AutoIPAMMetalLB reserves one Prism address for a Service and announces that
// exact address with a MetalLB /32 IPAddressPool. It does not add or remove the
// Service cleanup finalizer; the Service controller owns that finalizer.
type AutoIPAMMetalLB struct {
	config config.AutoIPAMMetalLB
	deps   Dependencies
}

var _ Strategy = (*AutoIPAMMetalLB)(nil)

// Name returns the strategy name.
func (*AutoIPAMMetalLB) Name() string { return StrategyAutoIPAMMetalLB }

// GetLoadBalancer reports exists=true only when this strategy's IP annotation
// is present. A missing annotation is exists=false with a nil error: the
// Service has no reserved address. An error would make the Service
// controller abort cleanup and keep the load-balancer cleanup finalizer,
// without calling EnsureLoadBalancerDeleted. The create path does not consult
// this result; it calls EnsureLoadBalancer directly.
func (*AutoIPAMMetalLB) GetLoadBalancer(_ context.Context, _ string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	ip := serviceAnnotation(service, constants.LoadBalancerIPAnnotation)
	if ip == "" {
		// No reserved address. exists=false lets cleanup drop the finalizer.
		return nil, false, nil
	}
	return &v1.LoadBalancerStatus{
		Ingress: []v1.LoadBalancerIngress{{IP: ip}},
	}, true, nil
}

// EnsureLoadBalancer activates a LoadBalancer Service and returns its address.
func (s *AutoIPAMMetalLB) EnsureLoadBalancer(ctx context.Context, _ string, service *v1.Service, _ []*v1.Node) (*v1.LoadBalancerStatus, error) {
	if service == nil || service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return nil, fmt.Errorf("AutoIPAMMetalLB activate requires a LoadBalancer Service")
	}
	ip, err := s.activate(ctx, service)
	if err != nil {
		return nil, err
	}
	return &v1.LoadBalancerStatus{
		Ingress: []v1.LoadBalancerIngress{{IP: ip}},
	}, nil
}

// UpdateLoadBalancer returns ImplementedElsewhere. MetalLB owns host membership
// and advertisement; this strategy does not apply the node list.
func (*AutoIPAMMetalLB) UpdateLoadBalancer(context.Context, string, *v1.Service, []*v1.Node) error {
	return cloudprovider.ImplementedElsewhere
}

// EnsureLoadBalancerDeleted does not release the reservation. Service deletion
// is separate from activation. It returns an error and never returns
// ImplementedElsewhere, and it does not change the Service, the pool, or the
// Prism reservation.
func (*AutoIPAMMetalLB) EnsureLoadBalancerDeleted(context.Context, string, *v1.Service) error {
	return fmt.Errorf("%s service deletion is not implemented", StrategyAutoIPAMMetalLB)
}

func (s *AutoIPAMMetalLB) activate(ctx context.Context, service *v1.Service) (string, error) {
	if service == nil {
		return "", fmt.Errorf("service is required")
	}
	if s.config.Subnet == "" {
		return "", fmt.Errorf("loadBalancer.autoIPAMMetalLB.subnet is empty; set the Prism subnet extID before using strategy %s", StrategyAutoIPAMMetalLB)
	}
	if _, _, err := s.clients(); err != nil {
		return "", err
	}

	live, err := s.liveService(ctx, service)
	if err != nil {
		return "", err
	}
	uid, err := serviceUID(live, service)
	if err != nil {
		return "", err
	}

	reservationID := serviceAnnotation(live, constants.LoadBalancerReservationIDAnnotation)
	if reservationID == "" {
		reservationID = uuid.NewString()
		live, err = s.updateService(ctx, live.Namespace, live.Name, func(svc *v1.Service) {
			setServiceAnnotation(svc, constants.LoadBalancerReservationIDAnnotation, reservationID)
		})
		if err != nil {
			return "", fmt.Errorf("persist reservation id: %w", err)
		}
	}

	ip := serviceAnnotation(live, constants.LoadBalancerIPAnnotation)
	if ip == "" {
		prism, err := s.prism()
		if err != nil {
			return "", err
		}
		reserved, reserveErr := prism.ReserveIP(ctx, s.config.Subnet, reservationID, uid)
		if reserveErr != nil {
			// Keep the reservation id on every failure, including terminal Prism
			// errors. Clearing it made each Service-controller retry mint a new
			// request id and POST another reserve, leaking addresses.
			return "", reserveErr
		}
		ip = reserved
		live, err = s.updateService(ctx, live.Namespace, live.Name, func(svc *v1.Service) {
			setServiceAnnotation(svc, constants.LoadBalancerIPAnnotation, ip)
			setServiceAnnotation(svc, constants.LoadBalancerSubnetAnnotation, s.config.Subnet)
		})
		if err != nil {
			return "", fmt.Errorf("persist reserved IP: %w", err)
		}
	}

	if err := s.ensurePool(ctx, s.metalLBNamespace(), uid, ip, s.config.Subnet, reservationID); err != nil {
		return "", err
	}

	if _, err := s.updateService(ctx, live.Namespace, live.Name, func(svc *v1.Service) {
		setServiceAnnotation(svc, constants.MetalLBRequestedIPsAnnotation, ip)
	}); err != nil {
		return "", fmt.Errorf("set MetalLB requested IP: %w", err)
	}
	return ip, nil
}

func (s *AutoIPAMMetalLB) ensurePool(ctx context.Context, namespace, name, ip, subnet, reservationID string) error {
	_, dynamicClient, err := s.clients()
	if err != nil {
		return err
	}
	client := dynamicClient.Resource(ipAddressPoolGVR).Namespace(namespace)
	existingObj, err := client.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, err := unstructuredFromPool(newIPAddressPool(namespace, name, ip, subnet, reservationID))
		if err != nil {
			return fmt.Errorf("encode IPAddressPool %s/%s: %w", namespace, name, err)
		}
		if _, err = client.Create(ctx, created, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create IPAddressPool %s/%s: %w", namespace, name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get IPAddressPool %s/%s: %w", namespace, name, err)
	}
	existing, err := poolFromUnstructured(existingObj)
	if err != nil {
		return fmt.Errorf("decode IPAddressPool %s/%s: %w", namespace, name, err)
	}
	if !poolIdentityMatches(existing, name, reservationID, subnet, ip) {
		return fmt.Errorf("IPAddressPool %s/%s exists with a different identity and was left unchanged", namespace, name)
	}
	changed := false
	if !poolSpecReady(existing, ip) {
		existing.Spec.Addresses = []string{ip + "/32"}
		existing.Spec.AutoAssign = ptr.To(false)
		changed = true
	}
	if existing.Labels[constants.LoadBalancerPoolTypeLabel] != constants.LoadBalancerPoolTypeAutoIPAMMetalLB {
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		existing.Labels[constants.LoadBalancerPoolTypeLabel] = constants.LoadBalancerPoolTypeAutoIPAMMetalLB
		changed = true
	}
	if !changed {
		return nil
	}
	updated, err := unstructuredFromPool(existing)
	if err != nil {
		return fmt.Errorf("encode IPAddressPool %s/%s: %w", namespace, name, err)
	}
	if _, err := client.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update IPAddressPool %s/%s: %w", namespace, name, err)
	}
	return nil
}

func newIPAddressPool(namespace, name, ip, subnet, reservationID string) *metallbv1beta1.IPAddressPool {
	return &metallbv1beta1.IPAddressPool{
		TypeMeta: metav1.TypeMeta{
			APIVersion: metallbv1beta1.GroupVersion.String(),
			Kind:       "IPAddressPool",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.LoadBalancerServiceUIDLabel:    name,
				constants.LoadBalancerReservationIDLabel: reservationID,
				constants.LoadBalancerSubnetLabel:        subnet,
				constants.LoadBalancerIPLabel:            ip,
				constants.LoadBalancerPoolTypeLabel:      constants.LoadBalancerPoolTypeAutoIPAMMetalLB,
			},
		},
		Spec: metallbv1beta1.IPAddressPoolSpec{
			Addresses:  []string{ip + "/32"},
			AutoAssign: ptr.To(false),
		},
	}
}

func poolIdentityMatches(pool *metallbv1beta1.IPAddressPool, serviceUID, reservationID, subnet, ip string) bool {
	if pool == nil || serviceUID == "" || reservationID == "" || subnet == "" || ip == "" {
		return false
	}
	labels := pool.Labels
	return labels[constants.LoadBalancerServiceUIDLabel] == serviceUID &&
		labels[constants.LoadBalancerReservationIDLabel] == reservationID &&
		labels[constants.LoadBalancerSubnetLabel] == subnet &&
		labels[constants.LoadBalancerIPLabel] == ip
}

func poolSpecReady(pool *metallbv1beta1.IPAddressPool, ip string) bool {
	if pool == nil || len(pool.Spec.Addresses) != 1 || pool.Spec.Addresses[0] != ip+"/32" {
		return false
	}
	return pool.Spec.AutoAssign != nil && !*pool.Spec.AutoAssign
}

func unstructuredFromPool(pool *metallbv1beta1.IPAddressPool) (*unstructured.Unstructured, error) {
	if pool == nil {
		return nil, fmt.Errorf("IPAddressPool is nil")
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pool)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(metallbv1beta1.GroupVersion.WithKind("IPAddressPool"))
	return u, nil
}

func poolFromUnstructured(obj *unstructured.Unstructured) (*metallbv1beta1.IPAddressPool, error) {
	if obj == nil {
		return nil, fmt.Errorf("IPAddressPool unstructured is nil")
	}
	pool := &metallbv1beta1.IPAddressPool{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

func (s *AutoIPAMMetalLB) metalLBNamespace() string {
	if s.config.MetalLBNamespace == "" {
		return defaultMetalLBNamespace
	}
	return s.config.MetalLBNamespace
}

func (s *AutoIPAMMetalLB) clients() (kubernetes.Interface, dynamic.Interface, error) {
	if s.deps.Kube == nil || s.deps.Dynamic == nil {
		return nil, nil, fmt.Errorf("kubernetes clients are not initialized for strategy %s", StrategyAutoIPAMMetalLB)
	}
	return s.deps.Kube, s.deps.Dynamic, nil
}

func (s *AutoIPAMMetalLB) prism() (interfaces.Prism, error) {
	if s.deps.Prism != nil {
		return s.deps.Prism, nil
	}
	if s.deps.PrismClient == nil {
		return nil, fmt.Errorf("prism client is not initialized for strategy %s", StrategyAutoIPAMMetalLB)
	}
	prism, err := s.deps.PrismClient.Get()
	if err != nil {
		return nil, fmt.Errorf("get prism client: %w", err)
	}
	if prism == nil {
		return nil, fmt.Errorf("prism client is not initialized for strategy %s", StrategyAutoIPAMMetalLB)
	}
	return prism, nil
}

func (s *AutoIPAMMetalLB) liveService(ctx context.Context, service *v1.Service) (*v1.Service, error) {
	kube, _, err := s.clients()
	if err != nil {
		return nil, err
	}
	live, err := kube.CoreV1().Services(service.Namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get service %s/%s: %w", service.Namespace, service.Name, err)
	}
	return live, nil
}

func (s *AutoIPAMMetalLB) updateService(ctx context.Context, namespace, name string, mutate func(*v1.Service)) (*v1.Service, error) {
	kube, _, err := s.clients()
	if err != nil {
		return nil, err
	}
	current, err := kube.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get service %s/%s: %w", namespace, name, err)
	}
	mutate(current)
	updated, err := kube.CoreV1().Services(namespace).Update(ctx, current, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update service %s/%s: %w", namespace, name, err)
	}
	return updated, nil
}

func serviceUID(live, requested *v1.Service) (string, error) {
	uid := ""
	if live != nil {
		uid = string(live.UID)
	}
	if uid == "" && requested != nil {
		uid = string(requested.UID)
	}
	if uid == "" {
		return "", fmt.Errorf("service UID is required for strategy %s", StrategyAutoIPAMMetalLB)
	}
	if requested != nil && requested.UID != "" && live != nil && live.UID != "" && requested.UID != live.UID {
		return "", fmt.Errorf("service UID %q does not match %q", requested.UID, live.UID)
	}
	return uid, nil
}

func serviceAnnotation(service *v1.Service, key string) string {
	if service == nil || service.Annotations == nil {
		return ""
	}
	return service.Annotations[key]
}

func setServiceAnnotation(service *v1.Service, key, value string) {
	if service.Annotations == nil {
		service.Annotations = map[string]string{}
	}
	service.Annotations[key] = value
}
