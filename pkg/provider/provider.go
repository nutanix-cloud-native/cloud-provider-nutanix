package provider

import (
	"encoding/json"
	"io"
	"io/ioutil"

	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const providerName = "nutanix"

type NtnxCloud struct {
	Name string

	client clientset.Interface
	Config Config
}

func init() {
	cloudprovider.RegisterCloudProvider(providerName,
		func(config io.Reader) (cloudprovider.Interface, error) {
			return newNtnxCloud(config)
		})
}

func newNtnxCloud(config io.Reader) (cloudprovider.Interface, error) {

	bytes, err := ioutil.ReadAll(config)
	if err != nil {
		klog.Infof("Error in initializing karbon cloudprovid config %q\n", err)
		return nil, err
	}
	klog.Infoln(string(bytes))

	ntnx := &NtnxCloud{
		Name: providerName,
	}
	err = json.Unmarshal(bytes, &ntnx.Config)

	return ntnx, err
}

// Initialize cloudprovider
func (nc *NtnxCloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder,
	stopCh <-chan struct{}) {
	var err error
	klog.Info("Initializing client ...")
	nc.client, err = clientset.NewForConfig(clientBuilder.ConfigOrDie(""))
	if err != nil {
		klog.Fatal(err.Error())
	}
	klog.Infof("Client initialized")
}

// ProviderName returns the cloud provider ID.
func (nc *NtnxCloud) ProviderName() string {
	return nc.Name
}

// HasClusterID returns true if the cluster has a clusterID
func (nc *NtnxCloud) HasClusterID() bool {
	return true // TODO need cluster ID
}

func (nc *NtnxCloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return nc, false
}

func (nc *NtnxCloud) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

func (nc *NtnxCloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

func (nc *NtnxCloud) Zones() (cloudprovider.Zones, bool) {
	return nil, false
}
