package provider

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

var _ = Describe("Test Client", func() {
	var (
		kClient         *fake.Clientset
		config          config.Config
		nClient         nutanixClient
		informerFactory informers.SharedInformerFactory
	)

	BeforeEach(func() {
		kClient = fake.NewSimpleClientset()
		config = mock.GenerateMockConfig()
		informerFactory = informers.NewSharedInformerFactory(kClient, time.Minute)
		nClient = nutanixClient{
			config: config,
		}
	})

	Context("Test SetInformers", func() {
		It("should fail if invalid secret has been set", func() {
			nClient.SetInformers(informerFactory)
			Expect(nClient.sharedInformers).ToNot(BeNil())
		})
	})
})
