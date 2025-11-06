/*
Copyright 2022 Nutanix, Inc

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
		return ok, err
	}
	klog.V(1).InfoS("InstanceExists", "node", node.Name, "exists", ok) //nolint:typecheck
	return ok, err
}

func (i *instancesV2) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	ok, err := i.nutanixManager.isNodeShutdown(ctx, node)
	if err != nil {
		return ok, err
	}
	klog.V(1).InfoS("InstanceShutdown", "node", node.Name, "shutdown", ok) //nolint:typecheck
	return ok, err
}

func (i *instancesV2) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	md, err := i.nutanixManager.getInstanceMetadata(ctx, node)
	if err != nil {
		return md, err
	}
	klog.V(1).InfoS("InstanceMetadata", "node", node.Name, "metadata", md) //nolint:typecheck
	return md, err
}
