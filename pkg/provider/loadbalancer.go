package provider

import (
	"context"

	v1 "k8s.io/api/core/v1"
)

// get the status from the service Info.
// returning an error - keeps the intent alive
// bool indicates if an lb was found or not
func (nc *NtnxCloud) GetLoadBalancer(ct context.Context, clusterName string, service *v1.Service) (
	*v1.LoadBalancerStatus, bool, error) {
	return nil, false, nil
}

// GetLoadBalancerName returns the name of the load balancer. Implementations must treat the
// *v1.Service parameter as read-only and not modify it.
func (nc *NtnxCloud) GetLoadBalancerName(ct context.Context, clusterName string, service *v1.Service) string {
	return ""
}

// It adds an entry "create" into the internal method call record.
func (nc *NtnxCloud) EnsureLoadBalancer(ct context.Context,
	clusterName string, service *v1.Service, nodes []*v1.Node) (
	*v1.LoadBalancerStatus, error) {
	return nil, nil
}

func (nc *NtnxCloud) UpdateLoadBalancer(ct context.Context,
	clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return nil
}

// EnsureLoadBalancerDeleted is a test-spy implementation of LoadBalancer.EnsureLoadBalancerDeleted.
// It adds an entry "delete" into the internal method call record.
func (nc *NtnxCloud) EnsureLoadBalancerDeleted(ct context.Context, clusterName string,
	service *v1.Service) error {
	return nil
}
