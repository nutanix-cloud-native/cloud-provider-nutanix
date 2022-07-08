package main

import (
	"os"

	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/app"
	"k8s.io/cloud-provider/app/config"
	"k8s.io/cloud-provider/options"
	"k8s.io/component-base/cli"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	_ "github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider" // for "nutanix" provider registration
	_ "k8s.io/component-base/metrics/prometheus/clientgo"                   // for client metric registration
	_ "k8s.io/component-base/metrics/prometheus/version"                    // for version metric registration
)

func main() {
	ccmOptions, err := options.NewCloudControllerManagerOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	fss := cliflag.NamedFlagSets{}
	command := app.NewCloudControllerManagerCommand(ccmOptions,
		cloudInitializer, controllerInitializers(), fss, wait.NeverStop)
	code := cli.Run(command)
	os.Exit(code)
}

// If custom ClientNames are used, as below, then the controller will not use
// the API server bootstrapped RBAC, and instead will require it to be installed
// separately.
func controllerInitializers() map[string]app.ControllerInitFuncConstructor {
	controllerInitializers := app.DefaultInitFuncConstructors
	if constructor, ok := controllerInitializers["cloud-node"]; ok {
		constructor.InitContext.ClientName = "mycloud-external-cloud-node-controller"
		controllerInitializers["cloud-node"] = constructor
	}
	if constructor, ok := controllerInitializers["cloud-node-lifecycle"]; ok {
		constructor.InitContext.ClientName = "mycloud-external-cloud-node-lifecycle-controller"
		controllerInitializers["cloud-node-lifecycle"] = constructor
	}
	if constructor, ok := controllerInitializers["service"]; ok {
		constructor.InitContext.ClientName = "mycloud-external-service-controller"
		controllerInitializers["service"] = constructor
	}
	if constructor, ok := controllerInitializers["route"]; ok {
		constructor.InitContext.ClientName = "mycloud-external-route-controller"
		controllerInitializers["route"] = constructor
	}
	return controllerInitializers
}

func cloudInitializer(config *config.CompletedConfig) cloudprovider.Interface {
	cloudConfig := config.ComponentConfig.KubeCloudShared.CloudProvider

	// initialize cloud provider with the cloud provider name and config file provided
	cloud, err := cloudprovider.InitCloudProvider(cloudConfig.Name, cloudConfig.CloudConfigFile)
	if err != nil {
		klog.Fatalf("Cloud provider could not be initialized: %v", err)
	}
	if cloud == nil {
		klog.Fatalf("Cloud provider is nil")
	}

	if !cloud.HasClusterID() {
		if config.ComponentConfig.KubeCloudShared.AllowUntaggedCloud {
			klog.Warning("detected a cluster without a ClusterID.  A ClusterID will be required in the future.  Please tag your cluster to avoid any future issues")
		} else {
			klog.Fatalf("no ClusterID found.  A ClusterID is required for the cloud provider to function properly.  This check can be bypassed by setting the allow-untagged-cloud option")
		}
	}
	return cloud
}
