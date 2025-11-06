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

package interfaces

import (
	"context"

	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"k8s.io/client-go/informers"
)

type Client interface {
	Get() (Prism, error)
	SetInformers(sharedInformers informers.SharedInformerFactory)
}

type Prism interface {
	GetVM(ctx context.Context, vmUUID string) (*vmmModels.Vm, error)
	GetCluster(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error)
	ListAllCluster(ctx context.Context) ([]clusterModels.Cluster, error)
	GetCategory(ctx context.Context, categoryUUID string) (*prismModels.Category, error)
	GetClusterHost(ctx context.Context, clusterUuid string, hostUUID string) (*clusterModels.Host, error)
}
