package provider

import (
	"context"
	"fmt"
	"strings"

	prismgoclient "github.com/nutanix-cloud-native/prism-go-client"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	kubernetesEnv "github.com/nutanix-cloud-native/prism-go-client/environment/providers/kubernetes"
	envTypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/node/helpers"
	"k8s.io/klog/v2"
)

var (
	ErrEnvironmentNotReady = fmt.Errorf("Environment not initialized or ready yet")
)

type nutanixManager struct {
	client          clientset.Interface
	sharedInformers informers.SharedInformerFactory
	secretInformer  coreinformers.SecretInformer
	config          Config
	env             envTypes.Environment
}

func newNutanixManager(config Config) (*nutanixManager, error) {
	klog.V(1).Info("Creating new newNutanixManager")
	m := &nutanixManager{
		config: config,
	}
	return m, nil
}

func (nc *nutanixManager) setupEnvironment() {
	pc := nc.config.PrismCentral
	prismEndpoint := kubernetesEnv.NutanixPrismEndpoint{
		Address:  pc.Address,
		Port:     pc.Port,
		Insecure: pc.Insecure,
		CredentialRef: &kubernetesEnv.NutanixCredentialReference{
			Kind: kubernetesEnv.SecretKind,
			Namespace: func() string {
				if pc.CredentialRef.Namespace != "" {
					return pc.CredentialRef.Namespace
				}
				return defaultCCMSecretNamespace
			}(),
			Name: pc.CredentialRef.Name,
		},
	}
	nc.env = environment.NewEnvironment(kubernetesEnv.NewProvider(prismEndpoint,
		nc.secretInformer))
}

func (nc *nutanixManager) setInformers(sharedInformers informers.SharedInformerFactory) {
	nc.sharedInformers = sharedInformers
	nc.secretInformer = nc.sharedInformers.Core().V1().Secrets()
	hasSynced := nc.secretInformer.Informer().HasSynced
	if !hasSynced() {
		stopCh := context.Background().Done()
		go nc.secretInformer.Informer().Run(stopCh)
		klog.Info("Waiting for secrets cache to sync")
		if ok := cache.WaitForCacheSync(stopCh, hasSynced); !ok {
			klog.Fatal("failed to wait for caches to sync")
		}
		klog.Info("Secrets cache synced")
	}
	nc.setupEnvironment()
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
	nClient, err := n.getNutanixClient()
	if err != nil {
		return nil, err
	}
	vm, err := nClient.V3.GetVM(vmUUID)
	if err != nil {
		return nil, err
	}

	klog.V(1).Infof("fetching nodeAddresses for node %s", nodeName)
	nodeAddresses, err := n.getNodeAddresses(ctx, vm)
	if err != nil {
		return nil, err
	}

	topologyInfo, err := n.getTopologyInfo(nClient, vm)
	if err != nil {
		return nil, err
	}

	klog.V(1).Infof("adding custom labels %s", nodeName)
	err = n.addCustomLabelsToNode(ctx, node)
	if err != nil {
		return nil, err
	}
	return &cloudprovider.InstanceMetadata{
		ProviderID:    providerID,
		InstanceType:  instanceType,
		NodeAddresses: nodeAddresses,
		Region:        topologyInfo.Region,
		Zone:          topologyInfo.Zone,
	}, nil
}

func (n *nutanixManager) addCustomLabelsToNode(ctx context.Context, node *v1.Node) error {
	labels := map[string]string{}
	nClient, err := n.getNutanixClient()
	if err != nil {
		return err
	}
	providerID, err := n.getNutanixProviderIDForNode(ctx, node)
	if err != nil {
		return err
	}
	vmUUID := n.stripNutanixIDFromProviderID(providerID)
	vm, err := nClient.V3.GetVM(vmUUID)
	if err != nil {
		return err
	}
	if vm.Status.ClusterReference != nil &&
		vm.Status.ClusterReference.UUID != nil &&
		vm.Status.ClusterReference.Name != nil {
		labels[customPEUUIDLabel] = *vm.Status.ClusterReference.UUID
		labels[customPENameLabel] = *vm.Status.ClusterReference.Name
	}
	if vm.Status.Resources.HostReference != nil &&
		vm.Status.Resources.HostReference.UUID != nil &&
		vm.Status.Resources.HostReference.Name != nil {
		labels[customHostUUIDLabel] = *vm.Status.Resources.HostReference.UUID
		labels[customHostNameLabel] = *vm.Status.Resources.HostReference.Name
	}

	result := helpers.AddOrUpdateLabelsOnNode(n.client, labels, node)
	if !result {
		return fmt.Errorf("error occurred while updating labels on node %s", node.Name)
	}
	return nil
}

func (n *nutanixManager) getTopologyCategories() TopologyCategories {
	topologyCategories := TopologyCategories{}
	configTopologyCategories := n.config.TopologyCategories
	if configTopologyCategories != nil {
		if configTopologyCategories.Region != "" {
			klog.V(1).Infof("using category key %s to detect region", configTopologyCategories.Region)
			topologyCategories.Region = configTopologyCategories.Region
		}
		if configTopologyCategories.Zone != "" {
			klog.V(1).Infof("using category key %s to detect zone", configTopologyCategories.Zone)
			topologyCategories.Zone = configTopologyCategories.Zone
		}
	}
	klog.V(1).Infof("Using category key %s to discover region and %s for zone", topologyCategories.Region, topologyCategories.Zone)
	return topologyCategories
}

func (n *nutanixManager) getNutanixClient() (*prismClientV3.Client, error) {
	if n.env == nil {
		return nil, ErrEnvironmentNotReady
	}
	me, err := n.env.GetManagementEndpoint(envTypes.Topology{})
	if err != nil {
		return nil, err
	}
	creds := &prismgoclient.Credentials{
		URL:      me.Address.Host, // Not really an URL
		Endpoint: me.Address.Host,
		Insecure: me.Insecure,
		Username: me.ApiCredentials.Username,
		Password: me.ApiCredentials.Password,
	}
	nutanixClient, err := prismClientV3.NewV3Client(*creds)
	if err != nil {
		return nil, err
	}

	_, err = nutanixClient.V3.GetCurrentLoggedInUser(context.Background())
	if err != nil {
		return nil, err
	}

	return nutanixClient, nil
}

func (n *nutanixManager) nodeExists(ctx context.Context, node *v1.Node) (bool, error) {
	vmUUID, err := n.getNutanixInstanceIDForNode(ctx, node)
	if err != nil {
		return false, err
	}
	nClient, err := n.getNutanixClient()
	if err != nil {
		return false, err
	}
	_, err = nClient.V3.GetVM(vmUUID)
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
	nClient, err := n.getNutanixClient()
	if err != nil {
		return false, err
	}
	vm, err := nClient.V3.GetVM(vmUUID)
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
	return *vm.Spec.Resources.PowerState == poweredOffState
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

	return fmt.Sprintf("%s://%s", ProviderName, strings.ToLower(vmUUID)), nil
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
	return strings.TrimPrefix(providerID, fmt.Sprintf("%s://", ProviderName))
}

func (n *nutanixManager) getTopologyInfo(nutanixClient *prismClientV3.Client, vm *prismClientV3.VMIntentResponse) (TopologyCategories, error) {
	tc := &TopologyCategories{}
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
	nClient, err := n.getNutanixClient()
	if err != nil {
		return *tc, err
	}

	klog.V(1).Infof("searching for topology info on cluster entity for VM: %s", *vm.Spec.Name)
	err = n.getTopologyInfoFromCluster(nClient, vm, tc)
	if err != nil {
		return *tc, err
	}
	klog.V(1).Infof("topology info after searching cluster: %+v", *tc)
	return *tc, nil
}

func (n *nutanixManager) getZoneInfoFromCategories(categories map[string]string, tc *TopologyCategories) {
	tCategories := n.getTopologyCategories()
	if r, ok := categories[tCategories.Region]; ok && tc.Region == "" {
		tc.Region = r
	}
	if z, ok := categories[tCategories.Zone]; ok && tc.Zone == "" {
		tc.Zone = z
	}
}

func (n *nutanixManager) getTopologyInfoFromCluster(nClient *prismClientV3.Client, vm *prismClientV3.VMIntentResponse, tc *TopologyCategories) error {
	if nClient == nil {
		return fmt.Errorf("nutanix client cannot be nil when searching for topology info")
	}
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if tc == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}
	clusterUUID := *vm.Status.ClusterReference.UUID
	cluster, err := nClient.V3.GetCluster(clusterUUID)
	if err != nil {
		return fmt.Errorf("error occurred while searching for topology info on cluster: %v", err)
	}
	n.getZoneInfoFromCategories(cluster.Metadata.Categories, tc)
	return nil
}

func (n *nutanixManager) getTopologyInfoFromVM(vm *prismClientV3.VMIntentResponse, tc *TopologyCategories) error {
	if vm == nil {
		return fmt.Errorf("vm cannot be nil when searching for topology info")
	}
	if tc == nil {
		return fmt.Errorf("topology categories cannot be nil when searching for topology info")
	}
	n.getZoneInfoFromCategories(vm.Metadata.Categories, tc)
	return nil
}

func (n *nutanixManager) hasEmptyTopologyInfo(tc TopologyCategories) bool {
	if tc.Zone == "" {
		return true
	}
	if tc.Region == "" {
		return true
	}
	return false
}
