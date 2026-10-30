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
	"errors"

	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
	multidomainModels "github.com/nutanix/ntnx-api-golang-clients/multidomain-go-client/v4/models/multidomain/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"k8s.io/client-go/informers"
)

type Client interface {
	Get() (Prism, error)
	SetInformers(sharedInformers informers.SharedInformerFactory)
}

var (
	// ErrIPTaskPending means a Prism IP reserve task did not reach a terminal
	// status before the wait timed out. Callers retry; a reserve retry must reuse
	// the same request id.
	ErrIPTaskPending = errors.New("prism ip task is pending")

	// ErrIPReservationTerminal means the reserve task failed and will not succeed
	// if the same request id is reused.
	ErrIPReservationTerminal = errors.New("prism ip reservation failed")
)

type Prism interface {
	IsProjectScoped(ctx context.Context) bool
	// ReserveIP reserves one address on subnetExtID and waits for the Prism task
	// to finish. requestID is the idempotency key. clientContext is stored with
	// the reservation. A wait timeout returns ErrIPTaskPending. A failed task
	// returns ErrIPReservationTerminal.
	ReserveIP(ctx context.Context, subnetExtID, requestID, clientContext string) (string, error)
	GetVM(ctx context.Context, vmUUID string) (*vmmModels.Vm, error)
	GetVMByBiosUUid(ctx context.Context, biosUUID string) (*vmmModels.Vm, error)
	GetCluster(ctx context.Context, clusterUUID string) (*clusterModels.Cluster, error)
	ListAllCluster(ctx context.Context) ([]clusterModels.Cluster, error)
	ListAllProject(ctx context.Context) ([]multidomainModels.Project, error)
	GetDefaultProject(ctx context.Context) (*multidomainModels.Project, error)
	GetDefaultProjectExtId(ctx context.Context) *string
	GetResourceGroups(ctx context.Context) ([]multidomainModels.ResourceGroup, error)
	ListDomainManagers(ctx context.Context) ([]prismModels.DomainManager, error)
	GetCategory(ctx context.Context, categoryUUID string) (*prismModels.Category, error)
	GetClusterHost(ctx context.Context, clusterUuid string, hostUUID string) (*clusterModels.Host, error)
	GetPrismCentralVersion(ctx context.Context) (string, error)
}
