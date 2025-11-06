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
	"net/netip"
	"strings"

	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"

	set "github.com/hashicorp/go-set/v3"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"go4.org/netipx"
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
	client         clientset.Interface
	config         config.Config
	nutanixClient  interfaces.Client
	ignoredNodeIPs *netipx.IPSet
}

func newNutanixManager(config config.Config) (*nutanixManager, error) {
	klog.V(1).Info("Creating new newNutanixManager") //nolint:typecheck

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
		nutanixClient: &nutanixClientEnvironment{
			config:      config,
			clientCache: convergedV4.NewClientCache(prismclientv4.WithSessionAuth(true)),
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
		klog.Fatal(err.Error()) //nolint:typecheck
	}
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		n.client, NoResyncPeriodFunc(), informers.WithNamespace(ccmNamespace))
	n.nutanixClient.SetInformers(informerFactory)

	klog.Infof("Set the informers with namespace %q", ccmNamespace) //nolint:typecheck
}

func (n *nutanixManager) getInstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	if node == nil {
		return nil, fmt.Errorf("node cannot be nil when fetching instance metadata")
	}

	nodeName := node.Name
	klog.V(1).Infof("fetching instance metadata for node %s", nodeName) //nolint:typecheck

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

	klog.V(1).Infof("fetching nodeAddresses for node %s", nodeName) //nolint:typecheck
	nodeAddresses := node.Status.Addresses
	if !n.isNodeAddressesSet(node) {
		nodeAddresses, err = n.getNodeAddresses(ctx, vm)
		if err != nil {
			return nil, err
		}
	}

	topologyInfo, err := n.getTopologyInfo(ctx, nClient, vm)
	if err != nil {
		return nil, err
	}

	if n.config.EnableCustomLabeling {
		klog.V(1).Infof("adding custom labels %s", nodeName) //nolint:typecheck
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
	var cluster *clusterModels.Cluster
	var host *clusterModels.Host

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

	if vm.Cluster != nil && vm.Cluster.ExtId != nil {
		cluster, err = nClient.GetCluster(ctx, *vm.Cluster.ExtId)
		if err != nil {
			return err
		}

		if vm.Host != nil && vm.Host.ExtId != nil {
			host, err = nClient.GetClusterHost(ctx, *vm.Cluster.ExtId, *vm.Host.ExtId)
			if err != nil {
				return err
			}
		}
	}

	if cluster != nil && cluster.ExtId != nil && cluster.Name != nil {
		labels[constants.CustomPEUUIDLabel] = *cluster.ExtId
		labels[constants.CustomPENameLabel] = *cluster.Name
	}

	if host != nil && host.ExtId != nil && host.HostName != nil {
		labels[constants.CustomHostUUIDLabel] = *host.ExtId
		labels[constants.CustomHostNameLabel] = *host.HostName
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
		klog.V(1).Infof("using category key %s to detect region", configTopologyCategories.RegionCategory) //nolint:typecheck
		topologyCategories.RegionCategory = configTopologyCategories.RegionCategory
	}
	if configTopologyCategories.ZoneCategory != "" {
		klog.V(1).Infof("using category key %s to detect zone", configTopologyCategories.ZoneCategory) //nolint:typecheck
		topologyCategories.ZoneCategory = configTopologyCategories.ZoneCategory
	}

	klog.V(1).Infof("Using category key %s to discover region and %s for zone", topologyCategories.RegionCategory, topologyCategories.ZoneCategory) //nolint:typecheck
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
		if !strings.Contains(fmt.Sprint(err), "VM_NOT_FOUND") {
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
	vm, err := nClient.GetVM(ctx, vmUUID)
	if err != nil {
		return false, err
	}
	if n.isVMShutdown(vm) {
		return true, nil
	}
	return false, nil
}

func (n *nutanixManager) isVMShutdown(vm *vmmModels.Vm) bool {
	return *vm.PowerState == vmmModels.POWERSTATE_OFF
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

func (n *nutanixManager) isNodeAddressesSet(node *v1.Node) bool {
	if node == nil {
		return false
	}

	var hasHostname, hasInternalIP bool
	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeHostName {
			hasHostname = true
		}

		if address.Type == v1.NodeInternalIP {
			hasInternalIP = true
		}
	}

	// We always set at least two address types: one internal IP and one hostname.
	// If either type is not found, then we have not set the node addresses.
	return hasHostname && hasInternalIP
}

func (n *nutanixManager) getNodeAddresses(_ context.Context, vm *vmmModels.Vm) ([]v1.NodeAddress, error) {
	var addressSet *set.Set[v1.NodeAddress]
	var addresses []v1.NodeAddress

	if vm == nil {
		return nil, fmt.Errorf("vm cannot be nil when getting node addresses")
	}

	if len(vm.Nics) == 0 {
		return nil, fmt.Errorf("unable to determine network interfaces from VM with UUID %s: vm has no nics", *vm.ExtId)
	}

	addressSet = set.From([]v1.NodeAddress{}) //nolint:typecheck
	for _, nic := range vm.Nics {
		if nic.NicNetworkInfo == nil {
			continue
		}

		switch nic.NicNetworkInfo.GetValue().(type) {
		case vmmModels.VirtualEthernetNicNetworkInfo:
			netInfo := nic.NicNetworkInfo.GetValue().(vmmModels.VirtualEthernetNicNetworkInfo)
			vmAddressSet, err := n.getNodeAddressesFromNicNetworkInfo(netInfo.Ipv4Config, netInfo.Ipv4Info)
			if err != nil {
				return nil, err
			}
			addressSet.InsertSlice(vmAddressSet)

		case vmmModels.DpOffloadNicNetworkInfo:
			netInfo := nic.NicNetworkInfo.GetValue().(vmmModels.DpOffloadNicNetworkInfo)
			vmAddressSet, err := n.getNodeAddressesFromNicNetworkInfo(netInfo.Ipv4Config, netInfo.Ipv4Info)
			if err != nil {
				return nil, err
			}
			addressSet.InsertSlice(vmAddressSet)

		default:
			klog.V(1).Infof("unsupported NIC network info type: %T", nic.NicNetworkInfo.GetValue()) //nolint:typecheck
			continue
		}
	}

	addresses = append(addresses, addressSet.Slice()...)

	if len(addresses) == 0 {
		return addresses, fmt.Errorf("unable to determine network interfaces from VM with UUID %s", *vm.ExtId)
	}

	addresses = append(addresses, v1.NodeAddress{
		Type:    v1.NodeHostName,
		Address: *vm.Name,
	})
	return addresses, nil
}

func (n *nutanixManager) getNodeAddressesFromNicNetworkInfo(ipv4Config *vmmModels.Ipv4Config, ipv4Info *vmmModels.Ipv4Info) ([]v1.NodeAddress, error) {
	addressSet := set.From([]v1.NodeAddress{})

	if ipv4Config != nil {
		primaryIP := ipv4Config.IpAddress.Value
		if primaryIP != nil {
			parsedIP, err := netip.ParseAddr(*primaryIP)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP address %q: %v", *primaryIP, err)
			}
			if !n.ignoredNodeIPs.Contains(parsedIP) {
				addressSet.Insert(v1.NodeAddress{
					Type:    v1.NodeInternalIP,
					Address: *primaryIP,
				})
			}
		}

		for _, ipAddress := range ipv4Config.SecondaryIpAddressList {
			if ipAddress.Value == nil {
				continue
			}
			parsedIP, err := netip.ParseAddr(*ipAddress.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP address %q: %v", *ipAddress.Value, err)
			}
			if !n.ignoredNodeIPs.Contains(parsedIP) {
				addressSet.Insert(v1.NodeAddress{
					Type:    v1.NodeInternalIP,
					Address: *ipAddress.Value,
				})
			}
		}
	}

	if ipv4Info != nil {
		for _, ipAddress := range ipv4Info.LearnedIpAddresses {
			if ipAddress.Value == nil {
				continue
			}

			parsedIP, err := netip.ParseAddr(*ipAddress.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP address %q: %v", *ipAddress.Value, err)
			}

			if !n.ignoredNodeIPs.Contains(parsedIP) {
				addressSet.Insert(v1.NodeAddress{
					Type:    v1.NodeInternalIP,
					Address: *ipAddress.Value,
				})
			}
		}
	}
	return addressSet.Slice(), nil
}

func (n *nutanixManager) stripNutanixIDFromProviderID(providerID string) string {
	return strings.TrimPrefix(providerID, fmt.Sprintf("%s://", constants.ProviderName))
}

func (n *nutanixManager) getTopologyInfo(ctx context.Context, nutanixClient interfaces.Prism, vm *vmmModels.Vm) (config.TopologyInfo, error) {
	topologyDiscovery := n.config.TopologyDiscovery

	switch topologyDiscovery.Type {
	case config.PrismTopologyDiscoveryType:
		return n.getTopologyInfoUsingPrism(ctx, nutanixClient, vm)
	case config.CategoriesTopologyDiscoveryType:
		return n.getTopologyInfoUsingCategories(ctx, nutanixClient, vm)
	}
	return config.TopologyInfo{}, fmt.Errorf("unsupported topology discovery type: %s", topologyDiscovery.Type)
}

func (n *nutanixManager) getTopologyInfoUsingPrism(ctx context.Context, nClient interfaces.Prism, vm *vmmModels.Vm) (config.TopologyInfo, error) {
	ti := config.TopologyInfo{}
	if nClient == nil {
		return ti, fmt.Errorf("nutanix client cannot be nil when searching for Prism topology info")
	}
	if vm == nil {
		return ti, fmt.Errorf("vm cannot be nil when searching for Prism topology info")
	}

	if vm.Cluster == nil || vm.Cluster.ExtId == nil || *vm.Cluster.ExtId == "" {
		return ti, fmt.Errorf("cannot determine Prism zone information for vm %s", *vm.ExtId)
	}

	pc, err := n.getPrismCentralCluster(ctx, nClient)
	if err != nil {
		return ti, err
	}

	cluster, err := nClient.GetCluster(ctx, *vm.Cluster.ExtId)
	if err != nil {
		return ti, err
	}

	ti.Region = *pc.Name
	ti.Zone = *cluster.Name
	return ti, nil
}

func (n *nutanixManager) getTopologyInfoUsingCategories(ctx context.Context, nutanixClient interfaces.Prism, vm *vmmModels.Vm) (config.TopologyInfo, error) {
	tc := &config.TopologyInfo{}
	if vm == nil {
		return *tc, fmt.Errorf("vm cannot be nil while getting topology info")
	}
	klog.V(1).Infof("searching for topology info on VM entity: %s", *vm.Name) //nolint:typecheck
	err := n.getTopologyInfoFromVM(ctx, nutanixClient, vm, tc)
	if err != nil {
		return *tc, err
	}
	if !n.hasEmptyTopologyInfo(*tc) {
		klog.V(1).Infof("topology info was found on VM entity: %+v", *tc) //nolint:typecheck
		return *tc, nil
	}
	klog.V(1).Infof("searching for topology info on host entity for VM: %s", *vm.Name) //nolint:typecheck
	nClient, err := n.nutanixClient.Get()
	if err != nil {
		return *tc, err
	}

	klog.V(1).Infof("searching for topology info on cluster entity for VM: %s", *vm.Name) //nolint:typecheck
	err = n.getTopologyInfoFromCluster(ctx, nClient, vm, tc)
	if err != nil {
		return *tc, err
	}
	klog.V(1).Infof("topology info after searching cluster: %+v", *tc) //nolint:typecheck
	return *tc, nil
}

func (n *nutanixManager) getZoneInfoFromCategories(ctx context.Context, nClient interfaces.Prism, categoryUUIDs []string, ti *config.TopologyInfo) error {
	prismCategories := make(map[string][]string)
	for _, categoryUUID := range categoryUUIDs {
		category, err := nClient.GetCategory(ctx, categoryUUID)
		if err != nil {
			return err
		}
		if _, ok := prismCategories[*category.Key]; !ok {
			prismCategories[*category.Key] = []string{}
		}
		prismCategories[*category.Key] = append(prismCategories[*category.Key], *category.Value)
	}

	tCategories, err := n.getTopologyCategories()
	if err != nil {
		return err
	}

	if r, ok := prismCategories[tCategories.RegionCategory]; ok && ti.Region == "" {
		if len(r) == 0 {
			return fmt.Errorf("region category %s has no values", tCategories.RegionCategory)
		}

		if len(r) != 1 {
			return fmt.Errorf("region category %s has multiple values", tCategories.RegionCategory)
		}

		ti.Region = r[0]
	}

	if z, ok := prismCategories[tCategories.ZoneCategory]; ok && ti.Zone == "" {
		if len(z) == 0 {
			return fmt.Errorf("zone category %s has no values", tCategories.ZoneCategory)
		}

		if len(z) != 1 {
			return fmt.Errorf("zone category %s has multiple values", tCategories.ZoneCategory)
		}

		ti.Zone = z[0]
	}

	return nil
}

func (n *nutanixManager) getTopologyInfoFromCluster(ctx context.Context, nClient interfaces.Prism, vm *vmmModels.Vm, ti *config.TopologyInfo) error {
	if nClient == nil {
		return fmt.Errorf("nutanix client cannot be nil when searching for topology info")
	}
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if ti == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}
	clusterUUID := *vm.Cluster.ExtId
	cluster, err := nClient.GetCluster(ctx, clusterUUID)
	if err != nil {
		return fmt.Errorf("error occurred while searching for topology info on cluster: %v", err)
	}
	if err = n.getZoneInfoFromCategories(ctx, nClient, cluster.Categories, ti); err != nil {
		return err
	}
	return nil
}

func (n *nutanixManager) getTopologyInfoFromVM(ctx context.Context, nClient interfaces.Prism, vm *vmmModels.Vm, ti *config.TopologyInfo) error {
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if ti == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}

	vmCategories := make([]string, 0)
	for _, category := range vm.Categories {
		if category.ExtId != nil {
			vmCategories = append(vmCategories, *category.ExtId)
		}
	}

	if err := n.getZoneInfoFromCategories(ctx, nClient, vmCategories, ti); err != nil {
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

func (n *nutanixManager) getPrismCentralCluster(ctx context.Context, nClient interfaces.Prism) (*clusterModels.Cluster, error) {
	if nClient == nil {
		return nil, fmt.Errorf("nutanix client cannot be nil when getting prism central cluster")
	}
	clusters, err := nClient.ListAllCluster(ctx)
	if err != nil {
		return nil, err
	}

	foundPCs := make([]*clusterModels.Cluster, 0)
	for _, cluster := range clusters {
		if n.hasPEClusterServiceEnabled(&cluster, clusterModels.SOFTWARETYPEREF_PRISM_CENTRAL) {
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
	return nil, fmt.Errorf("more than one Prism Central cluster ")
}

func (n *nutanixManager) hasPEClusterServiceEnabled(cluster *clusterModels.Cluster, serviceType clusterModels.SoftwareTypeRef) bool {
	if cluster == nil {
		return false
	}
	serviceList := cluster.Config.ClusterSoftwareMap
	for _, s := range serviceList {
		if s.SoftwareType != nil && *s.SoftwareType == serviceType {
			return true
		}
	}
	return false
}
