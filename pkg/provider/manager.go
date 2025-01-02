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
	"errors"
	"fmt"
	"k8s.io/utils/ptr"
	"net/netip"
	"strings"

	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"
	clustermgmtconfig "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	prismconfig "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/prism/v4/config"
	vmmconfig "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"go4.org/netipx"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/node/helpers"
	"k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

type nutanixManager struct {
	client         clientset.Interface
	config         config.Config
	nutanixClient  *nutanixClient
	ignoredNodeIPs *netipx.IPSet
}

func newNutanixManager(config config.Config) (*nutanixManager, error) {
	klog.V(1).Info("Creating new newNutanixManager")

	// Initialize the ignoredNodeIPs set
	ignoredIPsBuilder := netipx.IPSetBuilder{}
	for _, ip := range config.IgnoredNodeIPs {
		switch {
		case strings.Contains(ip, "-"):
			ipRange, err := netipx.ParseIPRange(ip)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ignoredNodeIPs IP range %q: %v", ip, err)
			}
			ignoredIPsBuilder.AddRange(ipRange)
		case strings.Contains(ip, "/"):
			prefix, err := netip.ParsePrefix(ip)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ignoredNodeIPs IP prefix %q: %v", ip, err)
			}
			ignoredIPsBuilder.AddPrefix(prefix)
		default:
			parsedIP, err := netip.ParseAddr(ip)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ignoredNodeIPs IP %q: %v", ip, err)
			}
			ignoredIPsBuilder.Add(parsedIP)
		}
	}

	ignoredIPSet, err := ignoredIPsBuilder.IPSet()
	if err != nil {
		return nil, fmt.Errorf("failed to build ignoredNodeIPs IP set: %v", err)
	}

	m := &nutanixManager{
		config: config,
		nutanixClient: &nutanixClient{
			config:      config,
			clientCache: prismclientv4.NewClientCache(prismclientv4.WithSessionAuth(true)),
		},
		ignoredNodeIPs: ignoredIPSet,
	}
	return m, nil
}

func (n *nutanixManager) setKubernetesClient(client clientset.Interface) {
	n.client = client
	n.setInformers()
}

func (n *nutanixManager) setInformers() {
	// Set the nutanixClient's informersFactory with the ccm namespace
	ccmNamespace, err := GetCCMNamespace()
	if err != nil {
		klog.Fatal(err.Error())
	}
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		n.client, NoResyncPeriodFunc(), informers.WithNamespace(ccmNamespace))
	n.nutanixClient.SetInformers(informerFactory)

	klog.Infof("Set the informers with namespace %q", ccmNamespace)
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

	getVMResp, err := nClient.VmApiInstance.GetVmById(ptr.To(vmUUID))
	if err != nil {
		return nil, err
	}

	vm, ok := getVMResp.GetData().(*vmmconfig.Vm)
	if !ok {
		return nil, fmt.Errorf("failed to cast VM entity to VM type")
	}

	klog.V(1).Infof("fetching nodeAddresses for node %s", nodeName)
	nodeAddresses, err := n.getNodeAddresses(vm)
	if err != nil {
		return nil, err
	}

	topologyInfo, err := n.getTopologyInfo(ctx, vm)
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
	getVMResp, err := nClient.VmApiInstance.GetVmById(&vmUUID)
	if err != nil {
		return err
	}

	vm, ok := getVMResp.GetData().(*vmmconfig.Vm)
	if !ok {
		return fmt.Errorf("failed to cast VM entity to VM type")
	}

	getClusterResp, err := nClient.ClustersApiInstance.GetClusterById(vm.Cluster.ExtId, nil)
	if err != nil {
		return fmt.Errorf("error occurred while fetching cluster for VM %s: %v", *vm.Name, err)
	}

	cluster, ok := getClusterResp.GetData().(*clustermgmtconfig.Cluster)
	if !ok {
		return fmt.Errorf("failed to cast cluster entity to cluster type")
	}

	if cluster != nil &&
		cluster.ExtId != nil &&
		cluster.Name != nil {
		labels[constants.CustomPEUUIDLabel] = *cluster.ExtId
		labels[constants.CustomPENameLabel] = *cluster.Name

		getHostResp, err := nClient.ClustersApiInstance.GetHostById(cluster.ExtId, vm.Host.ExtId)
		if err != nil {
			return fmt.Errorf("error occurred while fetching host for VM %s: %v", *vm.Name, err)
		}

		host, ok := getHostResp.GetData().(*clustermgmtconfig.Host)
		if !ok {
			return fmt.Errorf("failed to cast host entity to host type")
		}

		labels[constants.CustomHostUUIDLabel] = *host.ExtId
		labels[constants.CustomHostNameLabel] = *host.HostName
	}

	result := helpers.AddOrUpdateLabelsOnNode(n.client, labels, node)
	if !result {
		return fmt.Errorf("error occurred while updating labels on node %s", node.Name)
	}

	return nil
}

func (n *nutanixManager) getTopologyCategories() (*config.TopologyCategories, error) {
	configTopologyCategories := n.config.TopologyDiscovery.TopologyCategories
	if n.config.TopologyDiscovery.Type != config.CategoriesTopologyDiscoveryType {
		return nil, fmt.Errorf("cannot invoke getTopologyCategories if topology discovery type is not %s", config.CategoriesTopologyDiscoveryType)
	}

	if n.config.TopologyDiscovery.TopologyCategories == nil {
		return nil, fmt.Errorf("topologyCategories must be set when using categories to discover topology")
	}

	topologyCategories := &config.TopologyCategories{}
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

	_, err = nClient.VmApiInstance.GetVmById(&vmUUID)
	if err != nil {
		if !strings.Contains(fmt.Sprint(err), "ENTITY_NOT_FOUND") {
			return false, err
		}
		return false, nil
	}

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

	getVMResp, err := nClient.VmApiInstance.GetVmById(&vmUUID)
	if err != nil {
		return false, err
	}

	vm, ok := getVMResp.GetData().(*vmmconfig.Vm)
	if !ok {
		return false, fmt.Errorf("failed to cast VM entity to VM type")
	}

	if n.isVMShutdown(vm) {
		return true, nil
	}

	return false, nil
}

func (n *nutanixManager) isVMShutdown(vm *vmmconfig.Vm) bool {
	return vm.PowerState.GetName() == constants.PoweredOffState
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

func (n *nutanixManager) getNodeAddresses(vm *vmmconfig.Vm) ([]v1.NodeAddress, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when getting node addresses")
	}

	var nodeAddresses []v1.NodeAddress
	for _, nic := range vm.Nics {
		var addresses []v1.NodeAddress
		if nic.NetworkInfo.Ipv4Config.IpAddress != nil {
			if nic.NetworkInfo.Ipv4Config.IpAddress.Value != nil {
				parsedIP, err := netip.ParseAddr(*nic.NetworkInfo.Ipv4Config.IpAddress.Value)
				if err != nil {
					return nodeAddresses, fmt.Errorf("failed to parse IP address %q: %v", *nic.NetworkInfo.Ipv4Config.IpAddress.Value, err)
				}

				// Ignore the IP address if it is one of the specified ignoredNodeIPs.
				if !n.ignoredNodeIPs.Contains(parsedIP) {
					addresses = append(addresses, v1.NodeAddress{
						Type:    v1.NodeInternalIP,
						Address: *nic.NetworkInfo.Ipv4Config.IpAddress.Value,
					})
				}
			}
		}

		for _, ipEndpoint := range nic.NetworkInfo.Ipv4Config.SecondaryIpAddressList {
			if ipEndpoint.Value != nil {
				parsedIP, err := netip.ParseAddr(*ipEndpoint.Value)
				if err != nil {
					return nodeAddresses, fmt.Errorf("failed to parse IP address %q: %v", *ipEndpoint.Value, err)
				}
				// Ignore the IP address if it is one of the specified ignoredNodeIPs.
				if !n.ignoredNodeIPs.Contains(parsedIP) {
					addresses = append(addresses, v1.NodeAddress{
						Type:    v1.NodeInternalIP,
						Address: *ipEndpoint.Value,
					})
				}
			}
		}

		// if no addresses were found assigned to the NIC, use the first IP address from learned IP addresses and assign it to the NIC
		if len(addresses) == 0 && len(nic.NetworkInfo.Ipv4Info.LearnedIpAddresses) > 0 {
			learnedIP := nic.NetworkInfo.Ipv4Info.LearnedIpAddresses[0]
			nc, err := n.nutanixClient.Get()
			if err != nil {
				return nil, err
			}

			resp, err := nc.VmApiInstance.AssignIpById(vm.ExtId, nic.ExtId, &vmmconfig.AssignIpParams{IpAddress: &learnedIP})
			if err != nil {
				return nil, err
			}

			task, ok := resp.GetData().(prismconfig.TaskReference)
			if !ok {
				return nil, fmt.Errorf("failed to assign IP address to VM %s", *vm.Name)
			}

			if task.ExtId == nil {
				return nil, fmt.Errorf("failed to assign IP address to VM %s", *vm.Name)
			}

			_, err = nc.TasksApiInstance.GetTaskById(task.ExtId, nil)
			if err != nil {
				switch {
				case errors.Is(err, ErrTaskOngoing),
					errors.Is(err, ErrTaskFailed),
					errors.Is(err, ErrTaskCancelled):
					return nodeAddresses, err
				default:
					return nodeAddresses, fmt.Errorf("failed to check task status: %w", err)
				}
			}

			addresses = append(addresses, v1.NodeAddress{
				Type:    v1.NodeInternalIP,
				Address: *learnedIP.Value,
			})
		}
	}

	if len(nodeAddresses) == 0 {
		return nodeAddresses, fmt.Errorf("unable to determine network interfaces from VM with UUID %s", *vm.ExtId)
	}

	nodeAddresses = append(nodeAddresses, v1.NodeAddress{
		Type:    v1.NodeHostName,
		Address: *vm.Name,
	})
	return nodeAddresses, nil
}

func (n *nutanixManager) stripNutanixIDFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, fmt.Sprintf("%s://", constants.ProviderName))
}

func (n *nutanixManager) getTopologyInfo(ctx context.Context, vm *vmmconfig.Vm) (*config.TopologyInfo, error) {
	topologyDiscovery := n.config.TopologyDiscovery

	switch topologyDiscovery.Type {
	case config.PrismTopologyDiscoveryType:
		return n.getTopologyInfoUsingPrism(ctx, vm)
	case config.CategoriesTopologyDiscoveryType:
		return n.getTopologyInfoUsingCategories(ctx, vm)
	}
	return nil, fmt.Errorf("unsupported topology discovery type: %s", topologyDiscovery.Type)
}

func (n *nutanixManager) getTopologyInfoUsingPrism(ctx context.Context, vm *vmmconfig.Vm) (*config.TopologyInfo, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when searching for Prism topology info")
	}

	if vm.Cluster == nil || *vm.Cluster.ExtId == "" {
		return nil, fmt.Errorf("cannot determine Prism zone information for vm %s", *vm.Name)
	}

	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return nil, err
	}

	getClusterResp, err := nClient.ClustersApiInstance.GetClusterById(vm.Cluster.ExtId, nil)
	if err != nil {
		return nil, fmt.Errorf("error occurred while fetching cluster for VM %s: %v", *vm.Name, err)
	}

	cluster, ok := getClusterResp.GetData().(*clustermgmtconfig.Cluster)
	if !ok {
		return nil, fmt.Errorf("failed to cast cluster entity to cluster type")
	}

	pc, err := n.getPrismCentralCluster(ctx)
	if err != nil {
		return nil, err
	}

	ti := &config.TopologyInfo{
		Region: pc.Spec.Name,
		Zone:   *cluster.Name,
	}

	return ti, nil
}

func (n *nutanixManager) getTopologyInfoUsingCategories(ctx context.Context, vm *vmmconfig.Vm) (*config.TopologyInfo, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil while getting topology info")
	}

	klog.V(1).Infof("searching for topology info on VM entity: %s", *vm.Name)
	tc, err := n.getTopologyInfoFromVM(vm)
	if err != nil {
		return nil, err
	}

	if !n.hasEmptyTopologyInfo(*tc) {
		klog.V(1).Infof("topology info was found on VM entity: %+v", *tc)
		return tc, nil
	}

	klog.V(1).Infof("searching for topology info on host entity for VM: %s", *vm.Name)
	klog.V(1).Infof("searching for topology info on cluster entity for VM: %s", *vm.Name)

	tc, err = n.getTopologyInfoFromCluster(ctx, vm)
	if err != nil {
		return nil, err
	}

	klog.V(1).Infof("topology info after searching cluster: %+v", *tc)
	return tc, nil
}

func (n *nutanixManager) getZoneInfoFromCategories(categories []string) (*config.TopologyInfo, error) {
	tCategories, err := n.getTopologyCategories()
	if err != nil {
		return nil, err
	}

	ti := &config.TopologyInfo{}
	if r, ok := categories[tCategories.RegionCategory]; ok && ti.Region == "" {
		ti.Region = r
	}

	if z, ok := categories[tCategories.ZoneCategory]; ok && ti.Zone == "" {
		ti.Zone = z
	}

	return ti, nil
}

func (n *nutanixManager) getTopologyInfoFromCluster(vm *vmmconfig.Vm) (*config.TopologyInfo, error) {
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return nil, err
	}

	if nClient == nil {
		return nil, fmt.Errorf("nutanix client cannot be nil when searching for topology info")
	}

	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when searching for topology info")
	}

	getClusterResp, err := nClient.ClustersApiInstance.GetClusterById(vm.Cluster.ExtId, nil)
	if err != nil {
		return nil, fmt.Errorf("error occurred while searching for topology info on cluster: %v", err)
	}

	cluster, ok := getClusterResp.GetData().(*clustermgmtconfig.Cluster)
	if !ok {
		return nil, fmt.Errorf("failed to cast cluster entity to cluster type")
	}

	return n.getZoneInfoFromCategories(cluster.Categories)
}

func (n *nutanixManager) getTopologyInfoFromVM(vm *vmmconfig.Vm) (*config.TopologyInfo, error) {
	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when searching for topology info")
	}

	tc, err := n.getZoneInfoFromCategories(vm.Categories)
	if err != nil {
		return nil, err
	}

	return tc, nil
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

func (n *nutanixManager) getPrismCentralCluster() (*clustermgmtconfig.Cluster, error) {
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return nil, err
	}

	listClustersResp, err := nClient.ClustersApiInstance.ListClusters(nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	clusters, ok := listClustersResp.GetData().([]clustermgmtconfig.Cluster)
	if !ok {
		return nil, fmt.Errorf("failed to cast cluster entity to cluster type")
	}

	foundPCs := make([]*clustermgmtconfig.Cluster, 0)
	for _, cluster := range clusters {
		if n.hasPEClusterServiceEnabled(&cluster, constants.PrismCentralService) {
			foundPCs = append(foundPCs, &cluster)
		}
	}
	amountOfFoundPCs := len(foundPCs)
	if amountOfFoundPCs == 1 {
		return foundPCs[0], nil
	}

	if len(foundPCs) == 0 {
		return nil, fmt.Errorf("failed to retrieve Prism Central cluster")
	}

	return nil, fmt.Errorf("more than one Prism Central cluster found")
}

func (n *nutanixManager) hasPEClusterServiceEnabled(cluster *clustermgmtconfig.Cluster, serviceName string) bool {
	if cluster == nil {
		return false
	}

	serviceList := cluster.Config.ClusterSoftwareMap
	for _, s := range serviceList {
		if s.SoftwareType != nil && strings.ToUpper(s.SoftwareType.GetName()) == serviceName {
			return true
		}
	}

	return false
}
