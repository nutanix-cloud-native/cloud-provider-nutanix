//go:build integration

/*
Copyright 2026 Nutanix, Inc

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
	"strings"
	"time"

	"github.com/nutanix-cloud-native/prism-go-client/converged"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	"github.com/nutanix-cloud-native/prism-go-client/environment/providers/local"
	envtypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
	subnetModels "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/networking/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/interfaces"
)

// Ensure PrismFromLocalEnv stays aligned with interfaces.Prism.
var _ interfaces.Prism = (*nutanixClient)(nil)

type localEnvParams struct {
	env envtypes.Environment
}

func (p localEnvParams) Key() string { return "integration-pc" }

func (p localEnvParams) ManagementEndpoint() envtypes.ManagementEndpoint {
	me, err := p.env.GetManagementEndpoint(envtypes.Topology{})
	if err != nil || me == nil {
		return envtypes.ManagementEndpoint{}
	}
	return *me
}

// ConvergedClientFromLocalEnv builds a Prism Central converged client from the
// same NUTANIX_* environment variables used by e2e.
func ConvergedClientFromLocalEnv() (*convergedV4.Client, error) {
	if os.Getenv("NUTANIX_USERNAME") == "" {
		if err := os.Setenv("NUTANIX_USERNAME", strings.TrimSpace(os.Getenv("NUTANIX_USER"))); err != nil {
			return nil, fmt.Errorf("set NUTANIX_USERNAME: %w", err)
		}
	}
	params := localEnvParams{env: environment.NewEnvironment(local.NewProvider())}
	cache := convergedV4.NewClientCache()
	return cache.GetOrCreate(params)
}

// PrismFromLocalEnv returns interfaces.Prism backed by local env credentials.
func PrismFromLocalEnv() (interfaces.Prism, error) {
	client, err := ConvergedClientFromLocalEnv()
	if err != nil {
		return nil, err
	}
	return &nutanixClient{convergedClient: client}, nil
}

// SubnetExtIDByName looks up a Prism subnet extID by exact name.
func SubnetExtIDByName(ctx context.Context, client *convergedV4.Client, name string) (string, error) {
	if client == nil || client.Subnets == nil {
		return "", fmt.Errorf("prism subnet client is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("subnet name is required")
	}
	filter := fmt.Sprintf("name eq '%s'", strings.ReplaceAll(name, "'", "''"))
	subnets, err := client.Subnets.List(ctx, converged.WithFilter(filter))
	if err != nil {
		return "", fmt.Errorf("list subnet %q: %w", name, err)
	}
	if len(subnets) == 0 || subnets[0].ExtId == nil || *subnets[0].ExtId == "" {
		return "", fmt.Errorf("no subnet named %q found on Prism Central", name)
	}
	return *subnets[0].ExtId, nil
}

// UnreserveIPByContext releases a reservation by Service UID client context.
// Integration tests use this because product UnreserveIP is out of scope.
func UnreserveIPByContext(ctx context.Context, client *convergedV4.Client, subnetExtID, requestID, clientContext string) error {
	if client == nil || client.Subnets == nil {
		return fmt.Errorf("prism subnet client is not initialized")
	}
	spec := subnetModels.NewIpUnreserveSpec()
	unreserveType := subnetModels.UNRESERVETYPE_CONTEXT
	spec.UnreserveType = &unreserveType
	spec.ClientContext = ptr.To(clientContext)

	unreserveCtx := convergedV4.WithRequestID(ctx, requestID)
	taskRef, err := client.Subnets.UnreserveIpsBySubnetId(unreserveCtx, subnetExtID, spec)
	if err != nil {
		return fmt.Errorf("unreserve IP on subnet %s: %w", subnetExtID, err)
	}
	if taskRef == nil || taskRef.ExtId == nil || *taskRef.ExtId == "" {
		return fmt.Errorf("unreserve IP on subnet %s: empty task reference", subnetExtID)
	}
	return waitUnreserveTask(ctx, client, *taskRef.ExtId)
}

func waitUnreserveTask(ctx context.Context, client *convergedV4.Client, taskID string) error {
	if client.Tasks == nil {
		return fmt.Errorf("prism task client is not initialized")
	}
	return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		task, err := client.Tasks.Get(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("get unreserve task %s: %w", taskID, err)
		}
		if task == nil || task.Status == nil {
			return false, nil
		}
		switch *task.Status {
		case prismModels.TASKSTATUS_SUCCEEDED:
			return true, nil
		case prismModels.TASKSTATUS_FAILED, prismModels.TASKSTATUS_CANCELED, prismModels.TASKSTATUS_CANCELING:
			return false, fmt.Errorf("unreserve task %s finished with status %s", taskID, task.Status.GetName())
		default:
			return false, nil
		}
	})
}
