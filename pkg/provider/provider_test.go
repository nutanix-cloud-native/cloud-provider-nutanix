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
	"bytes"
	"encoding/json"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/testing/mock"
	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
)

var _ = Describe("Test Provider", func() {
	const mockReaderValue = "mock-reader"

	var (
		kClient   *fake.Clientset
		ntnxCloud NtnxCloud
		nClient   nutanixClient
		c         config.Config
	)

	BeforeEach(func() {
		kClient = fake.NewSimpleClientset()
		c = mock.GenerateMockConfig()
		nClient = nutanixClient{
			config: c,
		}
		ntnxCloud = NtnxCloud{
			name:   constants.ProviderName,
			config: c,
			manager: &nutanixManager{
				config:        c,
				nutanixClient: &nClient,
			},
			instancesV2: &instancesV2{},
		}
	})

	Context("Test SetInformers", func() {
		It("should set the informers", func() {
			informerFactory := informers.NewSharedInformerFactory(kClient, time.Minute)
			ntnxCloud.SetInformers(informerFactory)
			Expect(nClient.env).To(BeNil())
		})
	})

	Context("Test AddKubernetesClient", func() {
		It("should add the kubernetes client", func() {
			ntnxCloud.addKubernetesClient(kClient)
			Expect(ntnxCloud.client).To(Equal(kClient))
		})
	})

	Context("Test ProviderName", func() {
		It("should return the correct provider name", func() {
			n := ntnxCloud.ProviderName()
			Expect(n).To(Equal(constants.ProviderName))
		})
	})

	Context("Test HasClusterID", func() {
		It("should return true", func() {
			v := ntnxCloud.HasClusterID()
			Expect(v).To(BeTrue())
		})
	})

	Context("Test LoadBalancer", func() {
		It("should not support load balancer functionality", func() {
			nc, b := ntnxCloud.LoadBalancer()
			Expect(b).To(BeFalse())
			Expect(nc).To(Equal(&ntnxCloud))
		})
	})

	Context("Test Routes", func() {
		It("should not support routes functionality", func() {
			nc, b := ntnxCloud.Routes()
			Expect(b).To(BeFalse())
			Expect(nc).To(BeNil())
		})
	})

	Context("Test Clusters", func() {
		It("should not support clusters functionality", func() {
			nc, b := ntnxCloud.Clusters()
			Expect(b).To(BeFalse())
			Expect(nc).To(BeNil())
		})
	})

	Context("Test Zones", func() {
		It("should not support zones (v1) functionality", func() {
			nc, b := ntnxCloud.Zones()
			Expect(b).To(BeFalse())
			Expect(nc).To(BeNil())
		})
	})

	Context("Test Instances", func() {
		It("should not support instances (v1) functionality", func() {
			nc, b := ntnxCloud.Instances()
			Expect(b).To(BeFalse())
			Expect(nc).To(BeNil())
		})
	})

	Context("Test InstancesV2", func() {
		It("should support instancesv2 functionality", func() {
			nc, b := ntnxCloud.InstancesV2()
			Expect(b).To(BeTrue())
			Expect(nc).ToNot(BeNil())
		})
	})

	Context("Test NewNtnxCloud", func() {
		It("should error when invalid reader is passed", func() {
			invalidReader := bytes.NewReader([]byte(mockReaderValue))
			_, err := newNtnxCloud(invalidReader)
			Expect(err).To(HaveOccurred())
		})

		It("should fail topologyCategories are not set but discovery type is Categories", func() {
			config := config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: config.CategoriesTopologyDiscoveryType,
				},
			}
			cBytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			cReader := bytes.NewReader(cBytes)
			_, err = newNtnxCloud(cReader)
			Expect(err).To(HaveOccurred())
		})

		It("should fail if invalid topology discovery type is passed", func() {
			config := config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: "invalid",
				},
			}
			cBytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			cReader := bytes.NewReader(cBytes)
			_, err = newNtnxCloud(cReader)
			Expect(err).To(HaveOccurred())
		})

		It("should fail if invalid topology discovery type is passed", func() {
			config := config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: "invalid",
				},
			}
			cBytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			cReader := bytes.NewReader(cBytes)
			_, err = newNtnxCloud(cReader)
			Expect(err).To(HaveOccurred())
		})

		It("should default to Prism topology Discovery", func() {
			c := config.Config{
				TopologyDiscovery: config.TopologyDiscovery{},
			}
			cBytes, err := json.Marshal(c)
			Expect(err).ToNot(HaveOccurred())
			cReader := bytes.NewReader(cBytes)
			_, err = newNtnxCloud(cReader)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return valid NtnxCloud when valid reader is passed", func() {
			config := config.Config{
				TopologyDiscovery: config.TopologyDiscovery{
					Type: config.CategoriesTopologyDiscoveryType,
					TopologyCategories: &config.TopologyCategories{
						RegionCategory: mock.MockDefaultRegion,
						ZoneCategory:   mock.MockDefaultZone,
					},
				},
			}
			cJson, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			validReader := bytes.NewReader(cJson)
			_, err = newNtnxCloud(validReader)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func TestCloudProviderNutanix(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cloud Provider Nutanix unit-test Suite")
}
