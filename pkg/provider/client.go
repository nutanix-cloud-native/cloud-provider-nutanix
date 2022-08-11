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

	prismgoclient "github.com/nutanix-cloud-native/prism-go-client"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	kubernetesEnv "github.com/nutanix-cloud-native/prism-go-client/environment/providers/kubernetes"
	envTypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

const errEnvironmentNotReady = "Environment not initialized or ready yet"

type nutanixClient struct {
	env             envTypes.Environment
	config          config.Config
	secretInformer  coreinformers.SecretInformer
	sharedInformers informers.SharedInformerFactory
}

func (n *nutanixClient) Get() (interfaces.Prism, error) {
	if n.env == nil {
		return nil, fmt.Errorf(errEnvironmentNotReady)
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

	return nutanixClient.V3, nil
}

func (n *nutanixClient) setupEnvironment() {
	pc := n.config.PrismCentral
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
				return constants.DefaultCCMSecretNamespace
			}(),
			Name: pc.CredentialRef.Name,
		},
	}
	n.env = environment.NewEnvironment(kubernetesEnv.NewProvider(prismEndpoint,
		n.secretInformer))
}

func (n *nutanixClient) SetInformers(sharedInformers informers.SharedInformerFactory) {
	n.sharedInformers = sharedInformers
	n.secretInformer = n.sharedInformers.Core().V1().Secrets()
	hasSynced := n.secretInformer.Informer().HasSynced
	if !hasSynced() {
		stopCh := context.Background().Done()
		go n.secretInformer.Informer().Run(stopCh)
		klog.Info("Waiting for secrets cache to sync")
		if ok := cache.WaitForCacheSync(stopCh, hasSynced); !ok {
			klog.Fatal("failed to wait for caches to sync")
		}
		klog.Info("Secrets cache synced")
	}
	n.setupEnvironment()
}
