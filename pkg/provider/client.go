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
	"fmt"
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/prism-go-client/environment"
	credentialtypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	kubernetesenv "github.com/nutanix-cloud-native/prism-go-client/environment/providers/kubernetes"
	envtypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"
	prismcommonconfig "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/common/v1/config"
	prismconfig "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

const (
	errEnvironmentNotReady = "environment not initialized or ready yet"
)

var (
	// ErrTaskFailed is returned when a task has failed.
	ErrTaskFailed = fmt.Errorf("task failed")
	// ErrTaskCancelled is returned when a task has been cancelled.
	ErrTaskCancelled = fmt.Errorf("task cancelled")
	// ErrTaskOngoing is returned when a task is still ongoing.
	ErrTaskOngoing = fmt.Errorf("task ongoing")
)

type nutanixClient struct {
	env               envtypes.Environment
	config            config.Config
	secretInformer    coreinformers.SecretInformer
	sharedInformers   informers.SharedInformerFactory
	configMapInformer coreinformers.ConfigMapInformer
	clientCache       *prismclientv4.ClientCache
}

// Key returns the constant client name
// This implements the CachedClientParams interface of prism-go-client
func (n *nutanixClient) Key() string {
	return constants.ClientName
}

// ManagementEndpoint returns the management endpoint of the Nutanix cluster
// This implements the CachedClientParams interface of prism-go-client
func (n *nutanixClient) ManagementEndpoint() envtypes.ManagementEndpoint {
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

func (n *nutanixClient) Get() (*prismclientv4.Client, error) {
	if err := n.setupEnvironment(); err != nil {
		return nil, fmt.Errorf("%s: %w", errEnvironmentNotReady, err)
	}

	if n.clientCache == nil {
		return nil, fmt.Errorf("%s: client cache not initialized", errEnvironmentNotReady)
	}

	client, err := n.clientCache.GetOrCreate(n)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (n *nutanixClient) GetTaskData(taskID string) ([]prismcommonconfig.KVPair, error) {
	client, err := n.Get()
	if err != nil {
		return nil, err
	}

	task, err := client.TasksApiInstance.GetTaskById(ptr.To(taskID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", taskID, err)
	}

	taskData, ok := task.GetData().(prismconfig.Task)
	if !ok {
		return nil, fmt.Errorf("unexpected task data type %[1]T: %+[1]v", task.GetData())
	}

	marshaledTaskData, err := json.Marshal(taskData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task data: %w", err)
	}

	if taskData.Status == nil {
		return nil, fmt.Errorf("%w: %s", ErrTaskOngoing, string(marshaledTaskData))
	}

	switch *taskData.Status {
	case prismconfig.TASKSTATUS_SUCCEEDED:
		return taskData.CompletionDetails, nil
	case prismconfig.TASKSTATUS_FAILED:
		return nil, fmt.Errorf("%w: %s", ErrTaskFailed, string(marshaledTaskData))
	case prismconfig.TASKSTATUS_CANCELED:
		return nil, fmt.Errorf("%w: %s", ErrTaskCancelled, string(marshaledTaskData))
	default:
		return nil, fmt.Errorf("%w: %s", ErrTaskOngoing, string(marshaledTaskData))
	}
}


func (n *nutanixClient) setupEnvironment() error {
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

func (n *nutanixClient) SetInformers(sharedInformers informers.SharedInformerFactory) {
	n.sharedInformers = sharedInformers
	n.secretInformer = n.sharedInformers.Core().V1().Secrets()
	n.configMapInformer = n.sharedInformers.Core().V1().ConfigMaps()
	n.syncCache(n.secretInformer.Informer())
	n.syncCache(n.configMapInformer.Informer())
}

func (n *nutanixClient) syncCache(informer cache.SharedInformer) {
	hasSynced := informer.HasSynced
	if !hasSynced() {
		stopCh := context.Background().Done()
		go informer.Run(stopCh)
		if ok := cache.WaitForCacheSync(stopCh, hasSynced); !ok {
			klog.Fatal("failed to wait for caches to sync")
		}
	}
}
