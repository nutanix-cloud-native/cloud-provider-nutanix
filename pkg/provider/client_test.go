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

//nolint:typecheck // Test file uses ginkgo/gomega which typecheck doesn't understand well
package provider

import (
	"context"
	"os"
	"time"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	"github.com/nutanix-cloud-native/prism-go-client/environment/providers/local"
	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func unsetEnv(key string) {
	Expect(os.Unsetenv(key)).To(Succeed()) //nolint:typecheck
}

var _ = Describe("Test Client", func() { // nolint:typecheck
	var (
		kClient         *fake.Clientset
		config          config.Config
		nClient         nutanixClientEnvironment
		informerFactory informers.SharedInformerFactory
	)

	BeforeEach(func() { // nolint:typecheck
		kClient = fake.NewSimpleClientset(
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mock-cred",
					Namespace: "mock-namespace",
				},
				Data: map[string][]byte{
					credentials.KeyName: []byte(`[
  {
    "type": "basic_auth",
    "data": {
      "prismCentral":{
        "username": "user",
        "password": "password"
      }
    }
  }
]`),
				},
			},
		)
		config = mock.GenerateMockConfig()
		informerFactory = informers.NewSharedInformerFactory(kClient, time.Minute)
		nClient = nutanixClientEnvironment{
			config: config,
		}
	})

	Context("Test SetInformers", func() { //nolint:typecheck
		It("should fail if invalid secret has been set", func() { //nolint:typecheck
			nClient.SetInformers(informerFactory)
			Expect(nClient.sharedInformers).ToNot(BeNil()) //nolint:typecheck
		})
	})

	Context("Test Key", func() { //nolint:typecheck
		It("should return the client name", func() { //nolint:typecheck
			Expect(nClient.Key()).To(Equal(constants.ClientName)) //nolint:typecheck
		})
	})

	Context("Test ManagementEndpoint", func() { //nolint:typecheck
		BeforeEach(func() { //nolint:typecheck
			nClient = nutanixClientEnvironment{
				config: config,
			}
		})

		It("should return the empty management endpoint if env is uninitialized", func() { //nolint:typecheck
			Expect(nClient.ManagementEndpoint()).To(BeZero()) //nolint:typecheck
		})

		It("should return the empty management endpoint if env isn't properly initialized", func() { //nolint:typecheck
			p := local.NewProvider()
			nClient.env = p
			Expect(nClient.ManagementEndpoint()).To(BeZero()) //nolint:typecheck
		})

		It("should return the management endpoint", func() { //nolint:typecheck
			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			defer unsetEnv("NUTANIX_ENDPOINT")
			Expect(err).To(BeNil()) //nolint:typecheck
			err = os.Setenv("NUTANIX_USERNAME", "username")
			defer unsetEnv("NUTANIX_USERNAME")
			Expect(err).To(BeNil()) //nolint:typecheck
			err = os.Setenv("NUTANIX_PASSWORD", "password")
			defer unsetEnv("NUTANIX_PASSWORD")
			Expect(err).To(BeNil()) //nolint:typecheck
			nClient.env = p
			Expect(nClient.ManagementEndpoint()).ToNot(BeZero()) //nolint:typecheck
		})
	})

	Context("Test Get", func() {
		BeforeEach(func() { // nolint:typecheck
			nClient = nutanixClientEnvironment{
				config: config,
			}
		})

		It("should return error if env is uninitialized", func() { // nolint:typecheck
			client, err := nClient.Get()
			Expect(err).ToNot(BeNil())
			Expect(client).To(BeNil())
		})

		It("should return error if clientCache is uninitialized", func() { // nolint:typecheck
			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")
			nClient.env = p
			client, err := nClient.Get()
			Expect(err).ToNot(BeNil())
			Expect(client).To(BeNil())
		})

		It("should return an error when client creation fails", func() { // nolint:typecheck
			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")
			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).ToNot(BeNil())
			Expect(client).To(BeNil())
		})

		It("should return a client when client creation succeeds", func() { // nolint:typecheck
			previousGetProjectScopeAndDefaultProjectFn := getProjectScopeAndDefaultProjectFn
			previousGetPCVersionFn := getPCVersionFn
			getProjectScopeAndDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (bool, *multidomainModels.Project, error) {
				if os.Getenv("NUTANIX_ENDPOINT") == "prism.nutanix.com" {
					return false, &multidomainModels.Project{ExtId: ptr.To("default-project-id")}, nil
				}
				return previousGetProjectScopeAndDefaultProjectFn(ctx, convergedClient)
			}
			getPCVersionFn = func(ctx context.Context, convergedClient *convergedV4.Client) (string, error) {
				if os.Getenv("NUTANIX_ENDPOINT") == "prism.nutanix.com" {
					return "pc.7.6.0.0", nil
				}
				return previousGetPCVersionFn(ctx, convergedClient)
			}
			defer func() {
				getProjectScopeAndDefaultProjectFn = previousGetProjectScopeAndDefaultProjectFn
				getPCVersionFn = previousGetPCVersionFn
			}()

			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")

			err = os.Setenv("NUTANIX_USERNAME", "username")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_USERNAME")

			err = os.Setenv("NUTANIX_PASSWORD", "password")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_PASSWORD")

			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).To(BeNil())
			Expect(client).ToNot(BeNil())
		})

		It("should skip project checks when pc version is less than 7.6", func() { // nolint:typecheck
			previousIsProjectScopedFn := isProjectScopedFn
			previousGetDefaultProjectFn := getDefaultProjectFn
			previousGetProjectScopeAndDefaultProjectFn := getProjectScopeAndDefaultProjectFn
			previousGetPCVersionFn := getPCVersionFn
			isProjectScopedFn = func(ctx context.Context, convergedClient *convergedV4.Client) (bool, error) {
				Fail("isProjectScopedFn should not be called for PC < 7.6")
				return false, nil
			}
			getDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (*multidomainModels.Project, error) {
				Fail("getDefaultProjectFn should not be called for PC < 7.6")
				return nil, nil
			}
			getProjectScopeAndDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (bool, *multidomainModels.Project, error) {
				Fail("getProjectScopeAndDefaultProjectFn should not be called for PC < 7.6")
				return false, nil, nil
			}
			getPCVersionFn = func(ctx context.Context, convergedClient *convergedV4.Client) (string, error) {
				return "pc.7.5.0.1", nil
			}
			defer func() {
				isProjectScopedFn = previousIsProjectScopedFn
				getDefaultProjectFn = previousGetDefaultProjectFn
				getProjectScopeAndDefaultProjectFn = previousGetProjectScopeAndDefaultProjectFn
				getPCVersionFn = previousGetPCVersionFn
			}()

			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")

			err = os.Setenv("NUTANIX_USERNAME", "username")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_USERNAME")

			err = os.Setenv("NUTANIX_PASSWORD", "password")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_PASSWORD")

			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).To(BeNil())
			Expect(client).ToNot(BeNil())
			Expect(client.IsProjectScoped(context.Background())).To(BeFalse())
			Expect(client.GetDefaultProjectExtId(context.Background())).To(BeNil())
		})

		It("should set zero UUID default project when project scoped on PC >= 7.6", func() {
			previousGetProjectScopeAndDefaultProjectFn := getProjectScopeAndDefaultProjectFn
			previousGetPCVersionFn := getPCVersionFn
			getProjectScopeAndDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (bool, *multidomainModels.Project, error) {
				return true, &multidomainModels.Project{ExtId: ptr.To(zeroUUID)}, nil
			}
			getPCVersionFn = func(ctx context.Context, convergedClient *convergedV4.Client) (string, error) {
				return "pc.7.6.0.0", nil
			}
			defer func() {
				getProjectScopeAndDefaultProjectFn = previousGetProjectScopeAndDefaultProjectFn
				getPCVersionFn = previousGetPCVersionFn
			}()

			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")

			err = os.Setenv("NUTANIX_USERNAME", "username")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_USERNAME")

			err = os.Setenv("NUTANIX_PASSWORD", "password")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_PASSWORD")

			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).To(BeNil())
			Expect(client).ToNot(BeNil())
			Expect(client.IsProjectScoped(context.Background())).To(BeTrue())
			Expect(client.GetDefaultProjectExtId(context.Background())).ToNot(BeNil())
			Expect(*client.GetDefaultProjectExtId(context.Background())).To(Equal(zeroUUID))
		})

		It("should refresh project scope and default project on each call", func() {
			previousGetProjectScopeAndDefaultProjectFn := getProjectScopeAndDefaultProjectFn
			previousGetPCVersionFn := getPCVersionFn

			projectScoped := false
			getProjectScopeAndDefaultProjectFn = func(ctx context.Context, convergedClient *convergedV4.Client) (bool, *multidomainModels.Project, error) {
				if projectScoped {
					return true, &multidomainModels.Project{ExtId: ptr.To(zeroUUID)}, nil
				}
				return false, &multidomainModels.Project{ExtId: ptr.To("default-project-id")}, nil
			}
			getPCVersionFn = func(ctx context.Context, convergedClient *convergedV4.Client) (string, error) {
				return "pc.7.6.0.0", nil
			}
			defer func() {
				getProjectScopeAndDefaultProjectFn = previousGetProjectScopeAndDefaultProjectFn
				getPCVersionFn = previousGetPCVersionFn
			}()

			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_ENDPOINT")

			err = os.Setenv("NUTANIX_USERNAME", "username")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_USERNAME")

			err = os.Setenv("NUTANIX_PASSWORD", "password")
			Expect(err).To(BeNil())
			defer unsetEnv("NUTANIX_PASSWORD")

			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))

			client, err := nClient.Get()
			Expect(err).To(BeNil())
			Expect(client).ToNot(BeNil())

			Expect(client.IsProjectScoped(context.Background())).To(BeFalse())
			Expect(*client.GetDefaultProjectExtId(context.Background())).To(Equal("default-project-id"))

			projectScoped = true
			Expect(client.IsProjectScoped(context.Background())).To(BeTrue())
			Expect(*client.GetDefaultProjectExtId(context.Background())).To(Equal(zeroUUID))

			projectScoped = false
			Expect(client.IsProjectScoped(context.Background())).To(BeFalse())
			Expect(*client.GetDefaultProjectExtId(context.Background())).To(Equal("default-project-id"))
		})
	})

	Context("Test setupEnvironment", func() {
		It("should return nil if env is already initialized", func() { // nolint:typecheck
			nClient.env = local.NewProvider()
			Expect(nClient.setupEnvironment()).To(BeNil())
		})

		It("should return error if CCM namespace is not set", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "")
			Expect(err).To(BeNil())
			defer unsetEnv(constants.CCMNamespaceKey)

			Expect(nClient.setupEnvironment()).ToNot(BeNil())
		})

		It("should set the namespace for credential ref if not set", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "kube-system")
			Expect(err).To(BeNil())
			defer unsetEnv(constants.CCMNamespaceKey)

			nClient.config.PrismCentral.CredentialRef.Namespace = ""
			defer func() {
				nClient.config = mock.GenerateMockConfig()
			}()

			err = nClient.setupEnvironment()
			Expect(err).To(BeNil())
			Expect(nClient.config.PrismCentral.CredentialRef.Namespace).To(Equal("kube-system"))
		})

		It("should set the namespace for additional trust bundle if not set", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "kube-system")
			Expect(err).To(BeNil())
			defer unsetEnv(constants.CCMNamespaceKey)

			nClient.config = mock.GenerateMockConfig()
			nClient.config.PrismCentral.AdditionalTrustBundle = &credentials.NutanixTrustBundleReference{
				Kind: credentials.NutanixTrustBundleKindConfigMap,
				Name: "nutanix-trust-bundle",
			}

			defer func() {
				nClient.config = mock.GenerateMockConfig()
			}()

			err = nClient.setupEnvironment()
			Expect(err).To(BeNil())
			Expect(nClient.config.PrismCentral.AdditionalTrustBundle.Namespace).To(Equal("kube-system"))
		})

		It("should allow api_key-only credentials", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "mock-namespace")
			Expect(err).To(BeNil())
			defer unsetEnv(constants.CCMNamespaceKey)

			kClient = fake.NewSimpleClientset(
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mock-cred",
						Namespace: "mock-namespace",
					},
					Data: map[string][]byte{
						credentials.KeyName: []byte(`[
  {
    "type": "api_key",
    "data": {
      "prismCentral":{
        "apiKey": "test-api-key"
      }
    }
  }
]`),
					},
				},
			)
			informerFactory = informers.NewSharedInformerFactory(kClient, time.Minute)
			nClient.SetInformers(informerFactory)
			Expect(nClient.setupEnvironment()).To(BeNil())
			me, err := nClient.env.GetManagementEndpoint(nil)
			Expect(err).To(BeNil())
			Expect(me.Address.String()).To(Equal("https://mock-address:9440"))
			Expect(me.ApiCredentials.APIKey).To(Equal("test-api-key"))
			Expect(me.ApiCredentials.Username).To(Equal(""))
			Expect(me.ApiCredentials.Password).To(Equal(""))
		})

		It("should pick first credential type if multiple credentials are provided", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "mock-namespace")
			Expect(err).To(BeNil())
			defer unsetEnv(constants.CCMNamespaceKey)

			kClient = fake.NewSimpleClientset(
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mock-cred",
						Namespace: "mock-namespace",
					},
					Data: map[string][]byte{
						credentials.KeyName: []byte(`[
  {
    "type": "basic_auth",
    "data": {
      "prismCentral":{
        "username": "user",
        "password": "password"
      }
    }
  },
  {
    "type": "api_key",
    "data": {
      "prismCentral":{
        "apiKey": "test-api-key"
      }
    }
  }
]`),
					},
				},
			)
			informerFactory = informers.NewSharedInformerFactory(kClient, time.Minute)
			nClient.SetInformers(informerFactory)
			Expect(nClient.setupEnvironment()).To(BeNil())
			me, err := nClient.env.GetManagementEndpoint(nil)
			Expect(err).To(BeNil())
			Expect(me.Address.String()).To(Equal("https://mock-address:9440"))
			Expect(me.ApiCredentials.APIKey).To(Equal(""))
			Expect(me.ApiCredentials.Username).To(Equal("user"))
			Expect(me.ApiCredentials.Password).To(Equal("password"))
		})
	})
})
