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
	"os"
	"time"

	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	"github.com/nutanix-cloud-native/prism-go-client/environment/providers/local"
	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

var _ = Describe("Test Client", func() { // nolint:typecheck
	var (
		kClient         *fake.Clientset
		config          config.Config
		nClient         nutanixClientEnvironment
		informerFactory informers.SharedInformerFactory
	)

	BeforeEach(func() { // nolint:typecheck
		kClient = fake.NewSimpleClientset()
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
			defer os.Unsetenv("NUTANIX_ENDPOINT")
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
			defer os.Unsetenv("NUTANIX_ENDPOINT")
			nClient.env = p
			client, err := nClient.Get()
			Expect(err).ToNot(BeNil())
			Expect(client).To(BeNil())
		})

		It("should return an error when client creation fails", func() { // nolint:typecheck
			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer os.Unsetenv("NUTANIX_ENDPOINT")
			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).ToNot(BeNil())
			Expect(client).To(BeNil())
		})

		It("should return a client when client creation succeeds", func() { // nolint:typecheck
			p := local.NewProvider()
			err := os.Setenv("NUTANIX_ENDPOINT", "prism.nutanix.com")
			Expect(err).To(BeNil())
			defer os.Unsetenv("NUTANIX_ENDPOINT")

			err = os.Setenv("NUTANIX_USERNAME", "username")
			Expect(err).To(BeNil())
			defer os.Unsetenv("NUTANIX_USERNAME")

			err = os.Setenv("NUTANIX_PASSWORD", "password")
			Expect(err).To(BeNil())
			defer os.Unsetenv("NUTANIX_PASSWORD")

			nClient.env = p
			nClient.clientCache = convergedV4.NewClientCache(prismclientv4.WithSessionAuth(false))
			client, err := nClient.Get()
			Expect(err).To(BeNil())
			Expect(client).ToNot(BeNil())
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
			defer os.Unsetenv(constants.CCMNamespaceKey)

			Expect(nClient.setupEnvironment()).ToNot(BeNil())
		})

		It("should set the namespace for credential ref if not set", func() { // nolint:typecheck
			err := os.Setenv(constants.CCMNamespaceKey, "kube-system")
			Expect(err).To(BeNil())
			defer os.Unsetenv(constants.CCMNamespaceKey)

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
			defer os.Unsetenv(constants.CCMNamespaceKey)

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
	})
})
