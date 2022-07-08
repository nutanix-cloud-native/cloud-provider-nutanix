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
	klog.V(1).Infof("Instancesv2: InstanceExists")
	return i.nutanixManager.nodeExists(ctx, node)
}

func (i *instancesV2) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	klog.V(1).Infof("Instancesv2: InstanceShutdown")
	return i.nutanixManager.isNodeShutdown(ctx, node)
}

func (i *instancesV2) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	klog.V(1).Infof("Instancesv2: InstanceMetadata")
	return i.nutanixManager.getInstanceMetadata(ctx, node)
}
