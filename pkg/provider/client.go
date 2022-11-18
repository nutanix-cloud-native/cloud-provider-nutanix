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
	"os"

	prismgoclient "github.com/nutanix-cloud-native/prism-go-client"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	credentialTypes "github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
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

const errEnvironmentNotReady = "environment not initialized or ready yet"

type nutanixClient struct {
	env               *envTypes.Environment
	config            config.Config
	secretInformer    coreinformers.SecretInformer
	sharedInformers   informers.SharedInformerFactory
	configMapInformer coreinformers.ConfigMapInformer
}

func (n *nutanixClient) Get() (interfaces.Prism, error) {
	if err := n.setupEnvironment(); err != nil {
		return nil, fmt.Errorf("%s: %v", errEnvironmentNotReady, err)
	}
	env := *n.env
	me, err := env.GetManagementEndpoint(envTypes.Topology{})
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

	clientOpts := make([]prismClientV3.ClientOption, 0)
	if me.AdditionalTrustBundle != "" {
		clientOpts = append(clientOpts, prismClientV3.WithPEMEncodedCertBundle([]byte(me.AdditionalTrustBundle)))
	}

	nutanixClient, err := prismClientV3.NewV3Client(*creds, clientOpts...)
	if err != nil {
		return nil, err
	}

	_, err = nutanixClient.V3.GetCurrentLoggedInUser(context.Background())
	if err != nil {
		return nil, err
	}

	return nutanixClient.V3, nil
}

func (n *nutanixClient) setupEnvironment() error {
	if n.env != nil {
		return nil
	}
	ccmNamespace, err := n.getCCMNamespace()
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
		additionalTrustBundleRef.Kind == credentialTypes.NutanixTrustBundleKindConfigMap &&
		additionalTrustBundleRef.Namespace == "" {
		additionalTrustBundleRef.Namespace = ccmNamespace
	}

	env := environment.NewEnvironment(kubernetesEnv.NewProvider(pc,
		n.secretInformer, n.configMapInformer))
	n.env = &env
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

func (n *nutanixClient) getCCMNamespace() (string, error) {
	ns := os.Getenv(constants.CCMNamespaceKey)
	if ns == "" {
		return "", fmt.Errorf("failed to retrieve CCM namespace. Make sure %s env variable is set", constants.CCMNamespaceKey)
	}
	return ns, nil
}
