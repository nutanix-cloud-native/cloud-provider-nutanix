package provider

import (
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (nc *NtnxCloud) Instances() (cloudprovider.Instances, bool) {
	klog.Info("Instances")
	return nil, false
}

// InstancesV2 is an implementation for instances and should only be implemented by external cloud providers.
// Implementing InstancesV2 is behaviorally identical to Instances but is optimized to significantly reduce
// API calls to the cloud provider when registering and syncing nodes. Implementation of this interface will
// disable calls to the Zones interface. Also returns true if the interface is supported, false otherwise.
func (nc *NtnxCloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	klog.Info("InstancesV2")
	return nil, false
}
