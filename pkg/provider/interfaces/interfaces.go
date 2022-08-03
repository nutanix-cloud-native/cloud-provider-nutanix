package interfaces

import (
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/v3"
	"k8s.io/client-go/informers"
)

type Client interface {
	Get() (Prism, error)
	SetInformers(sharedInformers informers.SharedInformerFactory)
}

type Prism interface {
	GetVM(vmUUID string) (*prismClientV3.VMIntentResponse, error)
	GetCluster(clusterUUID string) (*prismClientV3.ClusterIntentResponse, error)
	ListAllCluster(filter string) (*prismClientV3.ClusterListIntentResponse, error)
}
