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
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
	"github.com/nutanix-cloud-native/prism-go-client/converged"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment/credentials"
	"github.com/nutanix-cloud-native/prism-go-client/environment/providers/local"
	prismclientv4 "github.com/nutanix-cloud-native/prism-go-client/v4"
	gpuModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/ahv/config"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	subnetModels "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/networking/v4/config"
	networkingprism "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/prism/v4/config"
	commonModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/common/v1/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	prismErrors "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/error"
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

func TestNutanixClientReserveIP(t *testing.T) {
	for _, projectScoped := range []bool{false, true} {
		projectScoped := projectScoped
		name := "cluster-wide"
		if projectScoped {
			name = "project-scoped"
		}
		t.Run(name, func(t *testing.T) {
			stubProjectScope(t, projectScoped)
			subnets := &fakeSubnets{taskID: "task-1"}
			if projectScoped {
				overlay := subnetModels.SUBNETTYPE_OVERLAY
				subnets.subnet = &subnetModels.Subnet{
					ExtId:        ptr.To("subnet-ext"),
					SubnetType:   &overlay,
					VpcReference: ptr.To("vpc-1"),
					ClusterName:  ptr.To("should-not-be-used"),
				}
			}
			tasks := &fakeTasks{task: succeededReserveTask("10.7.0.4")}
			clusters := &recordingClusters{}
			client := &nutanixClient{convergedClient: &convergedV4.Client{}}
			client.convergedClient.Subnets = subnets
			client.convergedClient.Tasks = tasks
			client.convergedClient.Clusters = clusters

			ip, err := client.ReserveIP(context.Background(), "subnet-ext", "request-1", "service-uid")
			if err != nil {
				t.Fatalf("ReserveIP: %v", err)
			}
			if ip != "10.7.0.4" {
				t.Fatalf("ip = %q, want 10.7.0.4", ip)
			}
			if subnets.reserveCalls != 1 {
				t.Fatalf("ReserveIpsBySubnetId calls = %d, want 1", subnets.reserveCalls)
			}
			if subnets.lastReserveSubnet != "subnet-ext" {
				t.Fatalf("reserved subnet = %q", subnets.lastReserveSubnet)
			}
			spec, ok := subnets.lastReserveSpec.(*subnetModels.IpReserveSpec)
			if !ok || spec.ReserveType == nil || *spec.ReserveType != subnetModels.RESERVETYPE_IP_ADDRESS_COUNT {
				t.Fatalf("reserve spec = %#v", subnets.lastReserveSpec)
			}
			if spec.Count == nil || *spec.Count != 1 {
				t.Fatalf("count = %#v, want 1", spec.Count)
			}
			if spec.ClientContext == nil || *spec.ClientContext != "service-uid" {
				t.Fatalf("clientContext = %#v", spec.ClientContext)
			}
			if got := ntnxRequestID(subnets.lastReserveCtx); got != "request-1" {
				t.Fatalf("request id = %q, want request-1", got)
			}
			if tasks.gets != 1 || tasks.lastID != "task-1" {
				t.Fatalf("Tasks.Get calls=%d id=%q", tasks.gets, tasks.lastID)
			}
			if clusters.gets != 0 || clusters.lists != 0 {
				t.Fatalf("cluster calls get=%d list=%d", clusters.gets, clusters.lists)
			}
			if projectScoped {
				if subnets.getCalls != 1 {
					t.Fatalf("Subnets.Get calls = %d, want 1", subnets.getCalls)
				}
				if subnets.subnet.SubnetType == nil || *subnets.subnet.SubnetType != subnetModels.SUBNETTYPE_OVERLAY {
					t.Fatal("project-scoped reserve did not use an overlay subnet")
				}
			} else if subnets.getCalls != 0 {
				t.Fatalf("cluster-wide Subnets.Get calls = %d, want 0", subnets.getCalls)
			}
		})
	}
}

func TestNutanixClientReserveIPProjectScopedSubnetNotVisible(t *testing.T) {
	stubProjectScope(t, true)
	subnets := &fakeSubnets{getErr: errors.New("subnet not found")}
	clusters := &recordingClusters{}
	client := &nutanixClient{convergedClient: &convergedV4.Client{}}
	client.convergedClient.Subnets = subnets
	client.convergedClient.Tasks = &fakeTasks{}
	client.convergedClient.Clusters = clusters

	if _, err := client.ReserveIP(context.Background(), "hidden", "request-1", "service-uid"); err == nil {
		t.Fatal("expected visibility error, got nil")
	}
	if subnets.reserveCalls != 0 {
		t.Fatal("reserved an invisible subnet")
	}
	if clusters.gets != 0 || clusters.lists != 0 {
		t.Fatalf("cluster calls get=%d list=%d", clusters.gets, clusters.lists)
	}
}

func TestNutanixClientReserveIPWaitsForSuccess(t *testing.T) {
	stubProjectScope(t, false)
	queued := prismModels.TASKSTATUS_QUEUED
	subnets := &fakeSubnets{taskID: "task-wait"}
	tasks := &fakeTasks{
		responses: []*prismModels.Task{
			{Status: &queued},
			succeededReserveTask("10.7.0.9"),
		},
	}
	client := &nutanixClient{convergedClient: &convergedV4.Client{}}
	client.convergedClient.Subnets = subnets
	client.convergedClient.Tasks = tasks

	ip, err := client.ReserveIP(context.Background(), "subnet-ext", "request-1", "service-uid")
	if err != nil {
		t.Fatalf("ReserveIP: %v", err)
	}
	if ip != "10.7.0.9" {
		t.Fatalf("ip = %q, want 10.7.0.9", ip)
	}
	if tasks.gets < 2 {
		t.Fatalf("Tasks.Get calls = %d, want at least 2", tasks.gets)
	}
}

func TestNutanixClientReserveIPPendingTimeout(t *testing.T) {
	stubProjectScope(t, false)
	status := prismModels.TASKSTATUS_QUEUED
	subnets := &fakeSubnets{taskID: "task-pending"}
	tasks := &fakeTasks{task: &prismModels.Task{Status: &status}}
	client := &nutanixClient{convergedClient: &convergedV4.Client{}}
	client.convergedClient.Subnets = subnets
	client.convergedClient.Tasks = tasks

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_, err := client.ReserveIP(ctx, "subnet-ext", "request-1", "service-uid")
	if !errors.Is(err, interfaces.ErrIPTaskPending) {
		t.Fatalf("expected pending error, got %v", err)
	}
	if tasks.gets < 2 {
		t.Fatalf("Tasks.Get calls = %d, want repeated polls", tasks.gets)
	}
}

func stubProjectScope(t *testing.T, scoped bool) {
	t.Helper()
	prevVersion := getPCVersionFn
	prevScope := getProjectScopeAndDefaultProjectFn
	t.Cleanup(func() {
		getPCVersionFn = prevVersion
		getProjectScopeAndDefaultProjectFn = prevScope
	})
	getPCVersionFn = func(context.Context, *convergedV4.Client) (string, error) {
		return "pc.7.6.0", nil
	}
	getProjectScopeAndDefaultProjectFn = func(context.Context, *convergedV4.Client) (bool, *multidomainModels.Project, error) {
		if scoped {
			return true, &multidomainModels.Project{ExtId: ptr.To(zeroUUID)}, nil
		}
		return false, &multidomainModels.Project{ExtId: ptr.To("default-project")}, nil
	}
}

func succeededReserveTask(ip string) *prismModels.Task {
	status := prismModels.TASKSTATUS_SUCCEEDED
	kv := commonModels.NewKVPair()
	// Match real Prism kSubnetReserveIp completionDetails.
	kv.Name = ptr.To("reserved_or_unreserved_ips")
	value := commonModels.NewOneOfKVPairValue()
	payload := `{"reserved_ips": ["` + ip + `"]}`
	if err := value.SetValue(payload); err != nil {
		panic(err)
	}
	kv.Value = value
	return &prismModels.Task{
		Status:            &status,
		CompletionDetails: []commonModels.KVPair{*kv},
	}
}

func TestReservedIPFromPrismCompletionDetails(t *testing.T) {
	ip := reservedIP(succeededReserveTask("10.23.131.195"))
	if ip != "10.23.131.195" {
		t.Fatalf("reservedIP = %q, want 10.23.131.195", ip)
	}
}

func ntnxRequestID(ctx context.Context) string {
	for i := 0; ctx != nil && i < 20; i++ {
		v := reflect.ValueOf(ctx)
		if v.Kind() != reflect.Ptr || v.IsNil() {
			return ""
		}
		elem := v.Elem()
		if elem.Kind() != reflect.Struct || elem.Type().Name() != "valueCtx" || elem.NumField() < 3 {
			return ""
		}
		valField := elem.Field(2)
		val := reflect.NewAt(valField.Type(), unsafe.Pointer(valField.UnsafeAddr())).Elem().Interface()
		if headers, ok := val.(map[string]string); ok {
			if id := headers["NTNX-Request-Id"]; id != "" {
				return id
			}
		}
		parent, _ := elem.Field(0).Interface().(context.Context)
		ctx = parent
	}
	return ""
}

type fakeSubnets struct {
	subnet            *subnetModels.Subnet
	getErr            error
	getCalls          int
	taskID            string
	reserveCalls      int
	lastReserveCtx    context.Context
	lastReserveSubnet string
	lastReserveSpec   any
}

func (f *fakeSubnets) Get(context.Context, string) (*subnetModels.Subnet, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.subnet == nil {
		return &subnetModels.Subnet{}, nil
	}
	return f.subnet, nil
}

func (f *fakeSubnets) List(context.Context, ...converged.ODataOption) ([]subnetModels.Subnet, error) {
	return nil, errors.New("list not implemented")
}

func (f *fakeSubnets) NewIterator(context.Context, ...converged.ODataOption) converged.Iterator[subnetModels.Subnet] {
	return nil
}

func (f *fakeSubnets) ReserveIpsBySubnetId(ctx context.Context, subnetExtID string, spec any) (*networkingprism.TaskReference, error) {
	f.reserveCalls++
	f.lastReserveCtx = ctx
	f.lastReserveSubnet = subnetExtID
	f.lastReserveSpec = spec
	return &networkingprism.TaskReference{ExtId: ptr.To(f.taskID)}, nil
}

func (f *fakeSubnets) UnreserveIpsBySubnetId(context.Context, string, any) (*networkingprism.TaskReference, error) {
	return nil, errors.New("unreserve not implemented")
}

func (f *fakeSubnets) ListReservedIpsBySubnetId(context.Context, string, ...converged.ODataOption) (any, error) {
	return nil, errors.New("list reserved ips not implemented")
}

type fakeTasks struct {
	task      *prismModels.Task
	responses []*prismModels.Task
	gets      int
	lastID    string
}

func (f *fakeTasks) Get(_ context.Context, uuid string) (*prismModels.Task, error) {
	f.gets++
	f.lastID = uuid
	if len(f.responses) > 0 {
		idx := f.gets - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		return f.responses[idx], nil
	}
	return f.task, nil
}

func (f *fakeTasks) GetWithSelect(context.Context, string, []string) (*prismModels.Task, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTasks) List(context.Context, ...converged.ODataOption) ([]prismModels.Task, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTasks) NewIterator(context.Context, ...converged.ODataOption) converged.Iterator[prismModels.Task] {
	return nil
}

func (f *fakeTasks) Cancel(context.Context, string) (*prismErrors.AppMessage, error) {
	return nil, errors.New("not implemented")
}

type recordingClusters struct {
	gets  int
	lists int
}

func (c *recordingClusters) Get(context.Context, string) (*clusterModels.Cluster, error) {
	c.gets++
	return nil, errors.New("GetCluster called")
}

func (c *recordingClusters) List(context.Context, ...converged.ODataOption) ([]clusterModels.Cluster, error) {
	c.lists++
	return nil, errors.New("ListAllCluster called")
}

func (c *recordingClusters) NewIterator(context.Context, ...converged.ODataOption) converged.Iterator[clusterModels.Cluster] {
	return nil
}

func (c *recordingClusters) ListClusterVirtualGPUs(context.Context, string, ...converged.ODataOption) ([]clusterModels.VirtualGpuProfile, error) {
	return nil, nil
}

func (c *recordingClusters) ListClusterPhysicalGPUs(context.Context, string, ...converged.ODataOption) ([]clusterModels.PhysicalGpuProfile, error) {
	return nil, nil
}

func (c *recordingClusters) ListAHVVirtualGPUProfiles(context.Context, string, ...converged.ODataOption) ([]gpuModels.VirtualGpuProfile, error) {
	return nil, nil
}

func (c *recordingClusters) ListAHVPhysicalGPUProfiles(context.Context, string, ...converged.ODataOption) ([]gpuModels.PhysicalGpuProfile, error) {
	return nil, nil
}

func (c *recordingClusters) GetClusterHost(context.Context, string, string) (*clusterModels.Host, error) {
	return nil, nil
}

func (c *recordingClusters) ListClusterHosts(context.Context, string, ...converged.ODataOption) ([]clusterModels.Host, error) {
	return nil, nil
}

func (c *recordingClusters) NewClusterHostsIterator(context.Context, string, ...converged.ODataOption) converged.Iterator[clusterModels.Host] {
	return nil
}

func (c *recordingClusters) ListAllHosts(context.Context, ...converged.ODataOption) ([]clusterModels.Host, error) {
	return nil, nil
}

func (c *recordingClusters) NewAllHostsIterator(context.Context, ...converged.ODataOption) converged.Iterator[clusterModels.Host] {
	return nil
}
