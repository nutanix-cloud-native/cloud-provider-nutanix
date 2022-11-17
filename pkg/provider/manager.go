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
	"fmt"
	"strings"

	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/node/helpers"
	"k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

type nutanixManager struct {
	client        clientset.Interface
	config        config.Config
	nutanixClient interfaces.Client
}

func newNutanixManager(config config.Config) (*nutanixManager, error) {
	klog.V(1).Info("Creating new newNutanixManager")
	m := &nutanixManager{
		config: config,
		nutanixClient: &nutanixClient{
			config: config,
		},
	}
	return m, nil
}

func (nc *nutanixManager) setInformers(sharedInformers informers.SharedInformerFactory) {
	nc.nutanixClient.SetInformers(sharedInformers)
}

func (nc *nutanixManager) setKubernetesClient(client clientset.Interface) {
	nc.client = client
}

func (n *nutanixManager) getInstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	if node == nil {
		return nil, fmt.Errorf("node cannot be nil when fetching instance metadata")
	}

	nodeName := node.Name
	klog.V(1).Infof("fetching instance metadata for node %s", nodeName)

	vmUUID, err := n.getNutanixInstanceIDForNode(ctx, node)
	if err != nil {
		return nil, err
	}

	providerID, err := n.generateProviderID(ctx, vmUUID)
	if err != nil {
		return nil, err
	}
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return nil, err
	}
	vm, err := nClient.GetVM(ctx, vmUUID)
	if err != nil {
		return nil, err
	}

	klog.V(1).Infof("fetching nodeAddresses for node %s", nodeName)
	nodeAddresses, err := n.getNodeAddresses(ctx, vm)
	if err != nil {
		return nil, err
	}

	topologyInfo, err := n.getTopologyInfo(ctx, nClient, vm)
	if err != nil {
		return nil, err
	}

	if n.config.EnableCustomLabeling {
		klog.V(1).Infof("adding custom labels %s", nodeName)
		err = n.addCustomLabelsToNode(ctx, node)
		if err != nil {
			return nil, err
		}
	}
	return &cloudprovider.InstanceMetadata{
		ProviderID:    providerID,
		InstanceType:  constants.InstanceType,
		NodeAddresses: nodeAddresses,
		Region:        topologyInfo.Region,
		Zone:          topologyInfo.Zone,
	}, nil
}

func (n *nutanixManager) addCustomLabelsToNode(ctx context.Context, node *v1.Node) error {
	labels := map[string]string{}
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return err
	}
	providerID, err := n.getNutanixProviderIDForNode(ctx, node)
	if err != nil {
		return err
	}
	vmUUID := n.stripNutanixIDFromProviderID(providerID)
	vm, err := nClient.GetVM(ctx, vmUUID)
	if err != nil {
		return err
	}
	if vm.Status.ClusterReference != nil &&
		vm.Status.ClusterReference.UUID != nil &&
		vm.Status.ClusterReference.Name != nil {
		labels[constants.CustomPEUUIDLabel] = *vm.Status.ClusterReference.UUID
		labels[constants.CustomPENameLabel] = *vm.Status.ClusterReference.Name
	}
	if vm.Status.Resources.HostReference != nil &&
		vm.Status.Resources.HostReference.UUID != nil &&
		vm.Status.Resources.HostReference.Name != nil {
		labels[constants.CustomHostUUIDLabel] = *vm.Status.Resources.HostReference.UUID
		labels[constants.CustomHostNameLabel] = *vm.Status.Resources.HostReference.Name
	}

	result := helpers.AddOrUpdateLabelsOnNode(n.client, labels, node)
	if !result {
		return fmt.Errorf("error occurred while updating labels on node %s", node.Name)
	}
	return nil
}

func (n *nutanixManager) getTopologyCategories() (config.TopologyCategories, error) {
	topologyCategories := config.TopologyCategories{}
	configTopologyCategories := n.config.TopologyDiscovery.TopologyCategories
	if n.config.TopologyDiscovery.Type != config.CategoriesTopologyDiscoveryType {
		return topologyCategories, fmt.Errorf("cannot invoke getTopologyCategories if topology discovery type is not %s", config.CategoriesTopologyDiscoveryType)
	}
	if n.config.TopologyDiscovery.TopologyCategories == nil {
		return topologyCategories, fmt.Errorf("topologyCategories must be set when using categories to discover topology")
	}

	if configTopologyCategories.RegionCategory != "" {
		klog.V(1).Infof("using category key %s to detect region", configTopologyCategories.RegionCategory)
		topologyCategories.RegionCategory = configTopologyCategories.RegionCategory
	}
	if configTopologyCategories.ZoneCategory != "" {
		klog.V(1).Infof("using category key %s to detect zone", configTopologyCategories.ZoneCategory)
		topologyCategories.ZoneCategory = configTopologyCategories.ZoneCategory
	}

	klog.V(1).Infof("Using category key %s to discover region and %s for zone", topologyCategories.RegionCategory, topologyCategories.ZoneCategory)
	return topologyCategories, nil
}

func (n *nutanixManager) nodeExists(ctx context.Context, node *v1.Node) (bool, error) {
	vmUUID, err := n.getNutanixInstanceIDForNode(ctx, node)
	if err != nil {
		return false, err
	}
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return false, err
	}
	_, err = nClient.GetVM(ctx, vmUUID)
	if err != nil {
		if !strings.Contains(fmt.Sprint(err), "ENTITY_NOT_FOUND") {
			return false, err
		}
		klog.Infof("Node %s does not exist!", node.Name)
		return false, nil
	}
	klog.Infof("Node %s exists!", node.Name)
	return true, nil
}

func (n *nutanixManager) isNodeShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	vmUUID, err := n.getNutanixInstanceIDForNode(ctx, node)
	if err != nil {
		return false, err
	}
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return false, err
	}
	vm, err := nClient.GetVM(ctx, vmUUID)
	if err != nil {
		return false, err
	}
	if n.isVMShutdown(vm) {
		klog.Infof("Node %s is shutdown!", node.Name)
		return true, nil
	}
	klog.Infof("Node %s is not shutdown!", node.Name)
	return false, nil
}

func (n *nutanixManager) isVMShutdown(vm *prismClientV3.VMIntentResponse) bool {
	return *vm.Spec.Resources.PowerState == constants.PoweredOffState
}

func (n *nutanixManager) getNutanixInstanceIDForNode(ctx context.Context, node *v1.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("node cannot be nil when getting nutanix instance ID for node")
	}

	nodeUUID := node.Status.NodeInfo.SystemUUID
	if nodeUUID == "" {
		return "", fmt.Errorf("failed to retrieve node UUID for node with name %s", node.Name)
	}
	return strings.ToLower(nodeUUID), nil
}

func (n *nutanixManager) getNutanixProviderIDForNode(ctx context.Context, node *v1.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("node cannot be nil when fetching providerID")
	}

	providerID := node.Spec.ProviderID
	if providerID == "" {
		vmUUID, err := n.getNutanixInstanceIDForNode(ctx, node)
		if err != nil {
			return "", err
		}
		providerID, err = n.generateProviderID(ctx, vmUUID)
		if err != nil {
			return "", err
		}
	}
	return providerID, nil
}

func (n *nutanixManager) generateProviderID(ctx context.Context, vmUUID string) (string, error) {
	if vmUUID == "" {
		return "", fmt.Errorf("VM UUID cannot be empty when generating nutanix provider ID for node")
	}

	return fmt.Sprintf("%s://%s", constants.ProviderName, strings.ToLower(vmUUID)), nil
}

func (n *nutanixManager) getNodeAddresses(ctx context.Context, vm *prismClientV3.VMIntentResponse) ([]v1.NodeAddress, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when getting node addresses")
	}
	addresses := make([]v1.NodeAddress, 0)
	foundIPs := 0
	for _, nic := range vm.Status.Resources.NicList {
		for _, ipEndpoint := range nic.IPEndpointList {
			if ipEndpoint.IP != nil {
				addresses = append(addresses, v1.NodeAddress{
					Type:    v1.NodeInternalIP,
					Address: *ipEndpoint.IP,
				})
				foundIPs++
			}
		}
	}
	if foundIPs == 0 {
		return addresses, fmt.Errorf("unable to determine network interfaces from VM with UUID %s", *vm.Metadata.UUID)
	}
	addresses = append(addresses, v1.NodeAddress{
		Type:    v1.NodeHostName,
		Address: *vm.Spec.Name,
	})
	return addresses, nil
}

func (n *nutanixManager) stripNutanixIDFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, fmt.Sprintf("%s://", constants.ProviderName))
}

func (n *nutanixManager) getTopologyInfo(ctx context.Context, nutanixClient interfaces.Prism, vm *prismClientV3.VMIntentResponse) (config.TopologyInfo, error) {
	topologyDiscovery := n.config.TopologyDiscovery

	switch topologyDiscovery.Type {
	case config.PrismTopologyDiscoveryType:
		return n.getTopologyInfoUsingPrism(ctx, nutanixClient, vm)
	case config.CategoriesTopologyDiscoveryType:
		return n.getTopologyInfoUsingCategories(ctx, nutanixClient, vm)
	}
	return config.TopologyInfo{}, fmt.Errorf("unsupported topology discovery type: %s", topologyDiscovery.Type)
}

func (n *nutanixManager) getTopologyInfoUsingPrism(ctx context.Context, nClient interfaces.Prism, vm *prismClientV3.VMIntentResponse) (config.TopologyInfo, error) {
	ti := config.TopologyInfo{}
	if nClient == nil {
		return ti, fmt.Errorf("nutanix client cannot be nil when searching for Prism topology info")
	}
	if vm == nil {
		return ti, fmt.Errorf("vm cannot be nil when searching for Prism topology info")
	}

	if vm.Status.ClusterReference == nil || *vm.Status.ClusterReference.Name == "" {
		return ti, fmt.Errorf("cannot determine Prism zone information for vm %s", *vm.Spec.Name)
	}

	pc, err := n.getPrismCentralCluster(ctx, nClient)
	if err != nil {
		return ti, err
	}
	ti.Region = *pc.Spec.Name
	ti.Zone = *vm.Status.ClusterReference.Name
	return ti, nil
}

func (n *nutanixManager) getTopologyInfoUsingCategories(ctx context.Context, nutanixClient interfaces.Prism, vm *prismClientV3.VMIntentResponse) (config.TopologyInfo, error) {
	tc := &config.TopologyInfo{}
	if vm == nil {
		return *tc, fmt.Errorf("vm cannot be nil while getting topology info")
	}
	klog.V(1).Infof("searching for topology info on VM entity: %s", *vm.Spec.Name)
	err := n.getTopologyInfoFromVM(vm, tc)
	if err != nil {
		return *tc, err
	}
	if !n.hasEmptyTopologyInfo(*tc) {
		klog.V(1).Infof("topology info was found on VM entity: %+v", *tc)
		return *tc, nil
	}
	klog.V(1).Infof("searching for topology info on host entity for VM: %s", *vm.Spec.Name)
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return *tc, err
	}

	klog.V(1).Infof("searching for topology info on cluster entity for VM: %s", *vm.Spec.Name)
	err = n.getTopologyInfoFromCluster(ctx, nClient, vm, tc)
	if err != nil {
		return *tc, err
	}
	klog.V(1).Infof("topology info after searching cluster: %+v", *tc)
	return *tc, nil
}

func (n *nutanixManager) getZoneInfoFromCategories(categories map[string]string, ti *config.TopologyInfo) error {
	tCategories, err := n.getTopologyCategories()
	if err != nil {
		return err
	}
	if r, ok := categories[tCategories.RegionCategory]; ok && ti.Region == "" {
		ti.Region = r
	}
	if z, ok := categories[tCategories.ZoneCategory]; ok && ti.Zone == "" {
		ti.Zone = z
	}
	return nil
}

func (n *nutanixManager) getTopologyInfoFromCluster(ctx context.Context, nClient interfaces.Prism, vm *prismClientV3.VMIntentResponse, ti *config.TopologyInfo) error {
	if nClient == nil {
		return fmt.Errorf("nutanix client cannot be nil when searching for topology info")
	}
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if ti == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}
	clusterUUID := *vm.Status.ClusterReference.UUID
	cluster, err := nClient.GetCluster(ctx, clusterUUID)
	if err != nil {
		return fmt.Errorf("error occurred while searching for topology info on cluster: %v", err)
	}
	if err = n.getZoneInfoFromCategories(cluster.Metadata.Categories, ti); err != nil {
		return err
	}
	return nil
}

func (n *nutanixManager) getTopologyInfoFromVM(vm *prismClientV3.VMIntentResponse, ti *config.TopologyInfo) error {
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if ti == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}
	if err := n.getZoneInfoFromCategories(vm.Metadata.Categories, ti); err != nil {
		return err
	}
	return nil
}

func (n *nutanixManager) hasEmptyTopologyInfo(ti config.TopologyInfo) bool {
	if ti.Zone == "" {
		return true
	}
	if ti.Region == "" {
		return true
	}
	return false
}

func (n *nutanixManager) getPrismCentralCluster(ctx context.Context, nClient interfaces.Prism) (*prismClientV3.ClusterIntentResponse, error) {
	const filter = ""
	if nClient == nil {
		return nil, fmt.Errorf("nutanix client cannot be nil when getting prism central cluster")
	}
	responsePEs, err := nClient.ListAllCluster(ctx, filter)
	if err != nil {
		return nil, err
	}

	foundPCs := make([]*prismClientV3.ClusterIntentResponse, 0)
	for _, s := range responsePEs.Entities {
		if n.hasPEClusterServiceEnabled(s, constants.PrismCentralService) {
			foundPCs = append(foundPCs, s)
		}
	}
	amountOfFoundPCs := len(foundPCs)
	if amountOfFoundPCs == 1 {
		return foundPCs[0], nil
	}
	if len(foundPCs) == 0 {
		return nil, fmt.Errorf("failed to retrieve Prism Central cluster")
	}
	return nil, fmt.Errorf("more than one Prism Central cluster ")
}

func (n *nutanixManager) hasPEClusterServiceEnabled(peCluster *prismClientV3.ClusterIntentResponse, serviceName string) bool {
	if peCluster.Status == nil ||
		peCluster.Status.Resources == nil ||
		peCluster.Status.Resources.Config == nil {
		return false
	}
	serviceList := peCluster.Status.Resources.Config.ServiceList
	for _, s := range serviceList {
		if s != nil && strings.ToUpper(*s) == serviceName {
			return true
		}
	}
	return false
}
