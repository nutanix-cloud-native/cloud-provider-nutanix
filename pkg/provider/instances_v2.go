package provider

import (
	"context"

	v1 "k8s.io/api/core/v1"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

type instancesV2 struct {
	nutanixManager *nutanixManager
}

func newInstancesV2(nutanixManager *nutanixManager) cloudprovider.InstancesV2 {
	return &instancesV2{
		nutanixManager: nutanixManager,
	}
}

func (i *instancesV2) InstanceExists(ctx context.Context, node *v1.Node) (bool, error) {
	ok, err := i.nutanixManager.nodeExists(ctx, node)
	if err != nil {
		klog.ErrorS(err, "InstanceExists failed", "node", node.Name)
		return ok, err
	}
	klog.V(1).InfoS("InstanceExists", "node", node.Name, "exists", ok)
	return ok, err
}

func (i *instancesV2) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	ok, err := i.nutanixManager.isNodeShutdown(ctx, node)
	if err != nil {
		klog.ErrorS(err, "InstanceShutdown failed", "node", node.Name)
		return ok, err
	}
	klog.V(1).InfoS("InstanceShutdown", "node", node.Name, "shutdown", ok)
	return ok, err
}

func (i *instancesV2) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	md, err := i.nutanixManager.getInstanceMetadata(ctx, node)
	if err != nil {
		klog.ErrorS(err, "InstanceMetadata failed", "node", node.Name)
		return md, err
	}
	klog.V(1).InfoS("InstanceMetadata", "node", node.Name, "metadata", md)
	return md, err
}
