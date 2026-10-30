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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	credentialtypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	kubernetesenv "github.com/nutanix-cloud-native/prism-go-client/environment/providers/kubernetes"
	envtypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
	"github.com/nutanix-cloud-native/prism-go-client/versionutils"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	klog "k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants" //nolint:typecheck
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	subnetModels "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/networking/v4/config"
	networkingprism "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/prism/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
)

const errEnvironmentNotReady = "environment not initialized or ready yet"
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// ipTaskPollInterval is how often ReserveIP re-reads a Prism task.
const ipTaskPollInterval = time.Second

// ipTaskWaitTimeout is the maximum time ReserveIP waits for a reserve task.
const ipTaskWaitTimeout = 2 * time.Minute

type nutanixClientEnvironment struct {
	env               envtypes.Environment
	config            config.Config
	secretInformer    coreinformers.SecretInformer
	sharedInformers   informers.SharedInformerFactory
	configMapInformer coreinformers.ConfigMapInformer
	clientCache       *convergedV4.ClientCache
}

// Key returns the constant client name
// This implements the CachedClientParams interface of prism-go-client
func (n *nutanixClientEnvironment) Key() string {
	return constants.ClientName
}

// ManagementEndpoint returns the management endpoint of the Nutanix cluster
// This implements the CachedClientParams interface of prism-go-client
func (n *nutanixClientEnvironment) ManagementEndpoint() envtypes.ManagementEndpoint {
	if n.env == nil {
		klog.Error("environment not initialized")
		return envtypes.ManagementEndpoint{}
	}

	mgmtEndpoint, err := n.env.GetManagementEndpoint(envtypes.Topology{})
	if err != nil {
		klog.Errorf("failed to get management endpoint: %s", err.Error())
		return envtypes.ManagementEndpoint{}
	}

	return *mgmtEndpoint
}

func (n *nutanixClientEnvironment) Get() (interfaces.Prism, error) {
	var projectScoped bool
	var defaultProject *multidomainModels.Project
	var err error

	ctx := context.Background()
	if err := n.setupEnvironment(); err != nil {
		return nil, fmt.Errorf("%s: %w", errEnvironmentNotReady, err)
	}

	if n.clientCache == nil {
		return nil, fmt.Errorf("%s: client cache not initialized", errEnvironmentNotReady)
	}

	convergedClient, err := n.clientCache.GetOrCreate(n)
	if err != nil {
		return nil, err
	}

	pcVersion, err := getPCVersionFn(ctx, convergedClient)
	if err != nil {
		return nil, err
	}

	if !pcVersionSupportsProjects(pcVersion) {
		projectScoped = false
		defaultProject = nil
	} else {
		projectScoped, defaultProject, err = getProjectScopeAndDefaultProjectFn(ctx, convergedClient)
		if err != nil {
			return nil, err
		}
	}

	client := &nutanixClient{
		convergedClient: convergedClient,
		projectScoped:   projectScoped,
		defaultProject:  defaultProject,
	}

	return client, nil
}

func (n *nutanixClientEnvironment) setupEnvironment() error {
	if n.env != nil {
		return nil
	}

	ccmNamespace, err := GetCCMNamespace()
	if err != nil {
		return err
	}

	pc := n.config.PrismCentral
	if pc.CredentialRef != nil {
		if pc.CredentialRef.Namespace == "" {
			pc.CredentialRef.Namespace = ccmNamespace
		}
	}

	additionalTrustBundleRef := pc.AdditionalTrustBundle
	if additionalTrustBundleRef != nil &&
		additionalTrustBundleRef.Kind == credentialtypes.NutanixTrustBundleKindConfigMap &&
		additionalTrustBundleRef.Namespace == "" {
		additionalTrustBundleRef.Namespace = ccmNamespace
	}

	n.env = environment.NewEnvironment(kubernetesenv.NewProvider(pc, n.secretInformer, n.configMapInformer))

	return nil
}

func (n *nutanixClientEnvironment) SetInformers(sharedInformers informers.SharedInformerFactory) {
	n.sharedInformers = sharedInformers
	n.secretInformer = n.sharedInformers.Core().V1().Secrets()
	n.configMapInformer = n.sharedInformers.Core().V1().ConfigMaps()
	n.syncCache(n.secretInformer.Informer())
	n.syncCache(n.configMapInformer.Informer())
}

func (n *nutanixClientEnvironment) syncCache(informer cache.SharedInformer) {
	hasSynced := informer.HasSynced
	if !hasSynced() {
		stopCh := context.Background().Done()
		go informer.Run(stopCh)
		if ok := cache.WaitForCacheSync(stopCh, hasSynced); !ok {
			klog.Fatal("failed to wait for caches to sync")
		}
	}
}

var isProjectScopedFn = isProjectScoped

var getDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (*multidomainModels.Project, error) {
	return convergedClient.Projects.GetDefaultProject(ctx)
}

var getProjectScopeAndDefaultProjectFn = getProjectScopeAndDefaultProject

var getPCVersionFn = func(ctx context.Context, convergedClient *convergedV4.Client) (string, error) {
	return convergedClient.DomainManager.GetPrismCentralVersion(ctx)
}

// minPCVersionForProjects is the minimum Prism Central version that exposes
// the project-scoped APIs.
var minPCVersionForProjects = versionutils.Parse("7.6")

// pcVersionSupportsProjects reports whether the Prism Central version exposes
// the project-scoped APIs, which it does from 7.6.
func pcVersionSupportsProjects(version string) bool {
	return versionutils.Parse(version).AtLeast(minPCVersionForProjects)
}

func isProjectScoped(ctx context.Context, convergedClient *convergedV4.Client) (bool, error) {
	defaultProject, err := getDefaultSystemProject(ctx, convergedClient)
	if err != nil {
		return false, err
	}

	return defaultProject == nil, nil
}

func getProjectScopeAndDefaultProject(ctx context.Context, convergedClient *convergedV4.Client) (bool, *multidomainModels.Project, error) {
	defaultProject, err := getDefaultSystemProject(ctx, convergedClient)
	if err != nil {
		return false, nil, err
	}

	if defaultProject == nil {
		return true, &multidomainModels.Project{ExtId: strPtr(zeroUUID)}, nil
	}

	if defaultProject.ExtId == nil || *defaultProject.ExtId == "" {
		defaultProject.ExtId = strPtr(zeroUUID)
	}

	return false, defaultProject, nil
}

func getDefaultSystemProject(ctx context.Context, convergedClient *convergedV4.Client) (*multidomainModels.Project, error) {
	projects, err := convergedClient.Projects.List(ctx)
	if err != nil {
		return nil, err
	}

	for i := range projects {
		project := &projects[i]
		if boolValue(project.IsDefault) && boolValue(project.IsSystemDefined) {
			return project, nil
		}
	}

	return nil, nil
}

func boolValue(v *bool) bool {
	return v != nil && *v
}

func strPtr(value string) *string {
	return &value
}

type nutanixClient struct {
	convergedClient *convergedV4.Client

	mu sync.RWMutex

	projectScoped  bool
	defaultProject *multidomainModels.Project
}

func (client *nutanixClient) GetVM(ctx context.Context, vmUUID string) (*vmmModels.Vm, error) {
	return client.convergedClient.VMs.Get(ctx, vmUUID)
}

func (client *nutanixClient) GetVMByBiosUUid(ctx context.Context, biosUUID string) (*vmmModels.Vm, error) {
	return client.convergedClient.VMs.GetVMByBiosUUID(ctx, biosUUID)
}

func (client *nutanixClient) GetCluster(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error) {
	return client.convergedClient.Clusters.Get(ctx, clusterUUID)
}

func (client *nutanixClient) ListAllCluster(ctx context.Context) ([]clusterModels.Cluster, error) {
	return client.convergedClient.Clusters.List(ctx)
}

func (client *nutanixClient) GetCategory(ctx context.Context, categoryUUID string) (*prismModels.Category, error) {
	return client.convergedClient.Categories.Get(ctx, categoryUUID)
}

func (client *nutanixClient) GetClusterHost(ctx context.Context, clusterUuid string, hostUUID string) (*clusterModels.Host, error) {
	return client.convergedClient.Clusters.GetClusterHost(ctx, clusterUuid, hostUUID)
}

func (client *nutanixClient) GetPrismCentralVersion(ctx context.Context) (string, error) {
	return client.convergedClient.DomainManager.GetPrismCentralVersion(ctx)
}

func (client *nutanixClient) ListDomainManagers(ctx context.Context) ([]prismModels.DomainManager, error) {
	return client.convergedClient.DomainManager.List(ctx)
}

func (client *nutanixClient) GetDefaultProject(ctx context.Context) (*multidomainModels.Project, error) {
	return client.convergedClient.Projects.GetDefaultProject(ctx)
}

func (client *nutanixClient) GetResourceGroups(ctx context.Context) ([]multidomainModels.ResourceGroup, error) {
	return client.convergedClient.ResourceGroups.List(ctx)
}

func (client *nutanixClient) ListAllProject(ctx context.Context) ([]multidomainModels.Project, error) {
	return client.convergedClient.Projects.List(ctx)
}

func (client *nutanixClient) IsProjectScoped(ctx context.Context) bool {
	if err := client.refreshProjectScopeState(ctx); err != nil {
		klog.Warningf("failed to refresh project scope, using last known value: %v", err)
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	return client.projectScoped
}

func (client *nutanixClient) GetDefaultProjectExtId(ctx context.Context) *string {
	if err := client.refreshProjectScopeState(ctx); err != nil {
		klog.Warningf("failed to refresh default project, using last known value: %v", err)
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.defaultProject != nil && client.defaultProject.ExtId != nil {
		return client.defaultProject.ExtId
	}
	return nil
}

func (client *nutanixClient) refreshProjectScopeState(ctx context.Context) error {
	pcVersion, err := getPCVersionFn(ctx, client.convergedClient)
	if err != nil {
		return err
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	if !pcVersionSupportsProjects(pcVersion) {
		client.projectScoped = false
		client.defaultProject = nil
		return nil
	}

	projectScoped, defaultProject, err := getProjectScopeAndDefaultProjectFn(ctx, client.convergedClient)
	if err != nil {
		return err
	}

	client.projectScoped = projectScoped
	client.defaultProject = defaultProject

	return nil
}

// ReserveIP reserves one address on subnetExtID. Cluster-wide and project-scoped
// clients both call the subnet address API. A project-scoped client confirms the
// subnet extID is visible first and does not resolve it through a cluster.
func (client *nutanixClient) ReserveIP(ctx context.Context, subnetExtID, requestID, clientContext string) (string, error) {
	if err := client.requireSubnetExtID(subnetExtID); err != nil {
		return "", err
	}
	if client.convergedClient == nil || client.convergedClient.Subnets == nil {
		return "", fmt.Errorf("prism subnet client is not initialized")
	}

	if client.IsProjectScoped(ctx) {
		if _, err := client.convergedClient.Subnets.Get(ctx, subnetExtID); err != nil {
			return "", fmt.Errorf("subnet %s is not visible to project-scoped credentials: %w", subnetExtID, err)
		}
	}

	spec := subnetModels.NewIpReserveSpec()
	reserveType := subnetModels.RESERVETYPE_IP_ADDRESS_COUNT
	count := int64(1)
	spec.ReserveType = &reserveType
	spec.Count = &count
	spec.ClientContext = strPtr(clientContext)

	reserveCtx := convergedV4.WithRequestID(ctx, requestID)
	taskRef, err := client.convergedClient.Subnets.ReserveIpsBySubnetId(reserveCtx, subnetExtID, spec)
	if err != nil {
		return "", fmt.Errorf("reserve IP on subnet %s: %w", subnetExtID, err)
	}
	taskID, err := taskExtID(taskRef)
	if err != nil {
		return "", fmt.Errorf("reserve IP on subnet %s: %w", subnetExtID, err)
	}
	return client.readIPTask(ctx, taskID)
}

func (client *nutanixClient) requireSubnetExtID(subnetExtID string) error {
	if subnetExtID == "" {
		return fmt.Errorf("subnet extID is required")
	}
	return nil
}

func taskExtID(taskRef *networkingprism.TaskReference) (string, error) {
	if taskRef == nil || taskRef.ExtId == nil || *taskRef.ExtId == "" {
		return "", fmt.Errorf("empty task reference")
	}
	return *taskRef.ExtId, nil
}

// readIPTask polls Tasks.Get until the reserve task succeeds or fails. A
// succeeded task must include the reserved address. If the wait times out while
// the task is still pending, it returns ErrIPTaskPending.
func (client *nutanixClient) readIPTask(ctx context.Context, taskID string) (string, error) {
	if client.convergedClient.Tasks == nil {
		return "", fmt.Errorf("prism task client is not initialized")
	}

	var ip string
	err := wait.PollUntilContextTimeout(ctx, ipTaskPollInterval, ipTaskWaitTimeout, true, func(ctx context.Context) (bool, error) {
		task, err := client.convergedClient.Tasks.Get(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("get task %s: %w", taskID, err)
		}
		if task == nil || task.Status == nil {
			return false, nil
		}

		switch *task.Status {
		case prismModels.TASKSTATUS_SUCCEEDED:
			ip = reservedIP(task)
			if ip == "" {
				return false, fmt.Errorf("%w: task %s succeeded without an IP address", interfaces.ErrIPReservationTerminal, taskID)
			}
			return true, nil
		case prismModels.TASKSTATUS_FAILED, prismModels.TASKSTATUS_CANCELED, prismModels.TASKSTATUS_CANCELING:
			return false, fmt.Errorf("%w: %s", interfaces.ErrIPReservationTerminal, taskFailureDetail(task))
		default:
			return false, nil
		}
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || wait.Interrupted(err) {
			return "", fmt.Errorf("%w: task %s did not complete: %w", interfaces.ErrIPTaskPending, taskID, err)
		}
		return "", err
	}
	return ip, nil
}

func taskFailureDetail(task *prismModels.Task) string {
	if task == nil {
		return "task failed"
	}
	parts := make([]string, 0, len(task.ErrorMessages)+1)
	for _, msg := range task.ErrorMessages {
		if msg.Message != nil && *msg.Message != "" {
			parts = append(parts, *msg.Message)
		}
	}
	if task.LegacyErrorMessage != nil && *task.LegacyErrorMessage != "" {
		parts = append(parts, *task.LegacyErrorMessage)
	}
	if len(parts) == 0 {
		return "task failed"
	}
	return strings.Join(parts, "; ")
}

// reservedIP reads the address Prism reports on a succeeded reserve task.
// Real PC payloads look like:
//
//	completionDetails: [{
//	  name: "reserved_or_unreserved_ips",
//	  value: "{\"reserved_ips\": [\"10.0.0.1\"]}"
//	}]
func reservedIP(task *prismModels.Task) string {
	if task == nil {
		return ""
	}
	for _, kv := range task.CompletionDetails {
		if kv.Value == nil || kv.Value.Discriminator == nil {
			continue
		}
		if ip := ipFromAny(kv.Value.GetValue()); ip != "" {
			return ip
		}
	}
	return ""
}

func ipFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return firstIPString(t)
	case []string:
		for _, s := range t {
			if ip := firstIPString(s); ip != "" {
				return ip
			}
		}
	case []any:
		for _, item := range t {
			if ip := ipFromAny(item); ip != "" {
				return ip
			}
		}
	case map[string]any:
		for _, key := range []string{"reserved_ips", "reservedIps", "ipAddresses", "ipv4Address", "value", "ip", "ipAddress", "ipv4"} {
			if ip := ipFromAny(t[key]); ip != "" {
				return ip
			}
		}
		for _, nested := range t {
			if ip := ipFromAny(nested); ip != "" {
				return ip
			}
		}
	case map[string]string:
		for _, key := range []string{"reserved_ips", "reservedIps", "ipAddresses", "ipv4Address", "value", "ip", "ipAddress", "ipv4"} {
			if ip := firstIPString(t[key]); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func firstIPString(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var decoded any
		if err := json.Unmarshal([]byte(s), &decoded); err == nil {
			return ipFromAny(decoded)
		}
	}
	return ""
}
