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
	"fmt"
	"io"
	"io/ioutil"

	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

type NtnxCloud struct {
	name string

	client      clientset.Interface
	config      config.Config
	manager     *nutanixManager
	instancesV2 cloudprovider.InstancesV2
}

func init() {
	cloudprovider.RegisterCloudProvider(constants.ProviderName,
		func(config io.Reader) (cloudprovider.Interface, error) {
			return newNtnxCloud(config)
		})
}

func newNtnxCloud(configReader io.Reader) (cloudprovider.Interface, error) {
	bytes, err := ioutil.ReadAll(configReader)
	if err != nil {
		klog.Infof("Error in initializing %s cloudprovid config %q\n", constants.ProviderName, err)
		return nil, err
	}

	nutanixConfig, err := config.NewConfigFromBytes(bytes)
	if err != nil {
		return nil, fmt.Errorf("error occurred while loading config file: %v", err)
	}
	nutanixManager, err := newNutanixManager(nutanixConfig)
	if err != nil {
		return nil, err
	}

	ntnx := &NtnxCloud{
		name:        constants.ProviderName,
		config:      nutanixConfig,
		manager:     nutanixManager,
		instancesV2: newInstancesV2(nutanixManager),
	}

	return ntnx, err
}

// Initialize cloudprovider
func (nc *NtnxCloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder,
	stopCh <-chan struct{},
) {
	klog.Info("Initializing client ...")
	nc.addKubernetesClient(clientBuilder.ClientOrDie("cloud-provider-nutanix"))
	klog.Infof("Client initialized")
}

// SetInformers sets the informer on the cloud object. Implements cloudprovider.InformerUser
func (nc *NtnxCloud) SetInformers(informerFactory informers.SharedInformerFactory) {
	nc.manager.setInformers(informerFactory)
}

func (nc *NtnxCloud) addKubernetesClient(kclient clientset.Interface) {
	nc.client = kclient
	nc.manager.setKubernetesClient(kclient)
}

// ProviderName returns the cloud provider ID.
func (nc *NtnxCloud) ProviderName() string {
	return nc.name
}

// HasClusterID returns true if the cluster has a clusterID
func (nc *NtnxCloud) HasClusterID() bool {
	return true
}

func (nc *NtnxCloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return nc, false
}

func (nc *NtnxCloud) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (nc *NtnxCloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (nc *NtnxCloud) Zones() (cloudprovider.Zones, bool) {
	klog.Info("Zones [DEPRECATED]")
	return nil, false
}

func (nc *NtnxCloud) Instances() (cloudprovider.Instances, bool) {
	klog.Info("Instances [DEPRECATED]")
	return nil, false
}

func (nc *NtnxCloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return nc.instancesV2, true
}
