//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
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
	networkingprism "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/prism/v4/config"
	prismModels "github.com/nutanix/ntnx-api-golang-clients/prism-go-client/v4/models/prism/v4/config"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

const (
	ipTaskPollInterval = time.Second
	ipTaskWaitTimeout  = 2 * time.Minute
)

// pcClientParams adapts the local (env-var-based) provider to envtypes.CachedClientParams,
// mirroring exactly how pkg/provider's nutanixClientEnvironment builds a converged client in
// production. The only difference from production is the credential source: env vars here vs. a
// Kubernetes Secret there.
type pcClientParams struct {
	env envtypes.Environment
}

func (p pcClientParams) Key() string { return "e2e-pc-lookup" }

func (p pcClientParams) ManagementEndpoint() envtypes.ManagementEndpoint {
	me, err := p.env.GetManagementEndpoint(envtypes.Topology{})
	if err != nil || me == nil {
		return envtypes.ManagementEndpoint{}
	}
	return *me
}

// resolveProjectAndResourceGroupUUIDs looks up the ext ID of the named Prism Central project and
// its associated resource group via the converged v4 client, using the base Nutanix connection
// variables already required by the e2e suite (NUTANIX_ENDPOINT/PORT/INSECURE/USER/PASSWORD).
// This avoids requiring testers to pre-compute and pass in UUIDs that Prism Central already knows.
//
// This function may run during Ginkgo tree construction (i.e. directly in a Describe body, not
// inside an It/BeforeEach), where Gomega's Expect/Must-style assertions panic instead of failing
// cleanly — so everything here deliberately sticks to plain error returns.
func resolveProjectAndResourceGroupUUIDs(ctx context.Context, projectName string) (projectUUID, resourceGroupUUID string, err error) {
	client, err := pcConvergedClient()
	if err != nil {
		return "", "", fmt.Errorf("failed to build Prism Central client: %w", err)
	}

	projects, err := client.Projects.List(ctx, converged.WithFilter(fmt.Sprintf("name eq '%s'", projectName)))
	if err != nil {
		return "", "", fmt.Errorf("failed to look up project %q on Prism Central: %w", projectName, err)
	}
	if len(projects) == 0 || projects[0].ExtId == nil {
		return "", "", fmt.Errorf("no project named %q found on Prism Central", projectName)
	}
	projectUUID = *projects[0].ExtId

	resourceGroups, err := client.ResourceGroups.List(ctx, converged.WithFilter(fmt.Sprintf("projectExtId eq '%s'", projectUUID)))
	if err != nil {
		return "", "", fmt.Errorf("failed to look up resource group for project %q on Prism Central: %w", projectName, err)
	}
	if len(resourceGroups) == 0 || resourceGroups[0].ExtId == nil {
		return "", "", fmt.Errorf("no resource group found for project %q (ext id %q) on Prism Central", projectName, projectUUID)
	}
	resourceGroupUUID = *resourceGroups[0].ExtId

	return projectUUID, resourceGroupUUID, nil
}

// resolveSubnetExtID looks up a Prism subnet extID by exact name.
func resolveSubnetExtID(ctx context.Context, name string) (string, error) {
	client, err := pcConvergedClient()
	if err != nil {
		return "", fmt.Errorf("failed to build Prism Central client: %w", err)
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

// reserveIPs reserves count addresses on subnetExtID under clientContext and returns the IPv4s.
// It waits for the Prism task, then lists reserved IPs for that context so multi-IP reserves work.
func reserveIPs(ctx context.Context, subnetExtID, requestID, clientContext string, count int64) ([]string, error) {
	if subnetExtID == "" {
		return nil, fmt.Errorf("subnet extID is required")
	}
	if clientContext == "" {
		return nil, fmt.Errorf("client context is required")
	}
	if count < 1 {
		return nil, fmt.Errorf("count must be at least 1")
	}
	client, err := pcConvergedClient()
	if err != nil {
		return nil, err
	}

	spec := subnetModels.NewIpReserveSpec()
	reserveType := subnetModels.RESERVETYPE_IP_ADDRESS_COUNT
	spec.ReserveType = &reserveType
	spec.Count = &count
	spec.ClientContext = ptr.To(clientContext)

	taskRef, err := client.Subnets.ReserveIpsBySubnetId(convergedV4.WithRequestID(ctx, requestID), subnetExtID, spec)
	if err != nil {
		return nil, fmt.Errorf("reserve %d IP(s) on subnet %s: %w", count, subnetExtID, err)
	}
	taskID, err := taskExtID(taskRef)
	if err != nil {
		return nil, fmt.Errorf("reserve IP on subnet %s: %w", subnetExtID, err)
	}
	if err := waitIPTask(ctx, client, taskID); err != nil {
		return nil, err
	}

	ips, err := reservedIPsByContext(ctx, subnetExtID, clientContext)
	if err != nil {
		return nil, err
	}
	if int64(len(ips)) < count {
		return nil, fmt.Errorf("reserved %d IP(s) for context %q, want at least %d", len(ips), clientContext, count)
	}
	return ips, nil
}

// unreserveIPsByContext releases every reservation stored under clientContext on subnetExtID.
func unreserveIPsByContext(ctx context.Context, subnetExtID, requestID, clientContext string) error {
	if subnetExtID == "" {
		return fmt.Errorf("subnet extID is required")
	}
	if clientContext == "" {
		return fmt.Errorf("client context is required")
	}
	client, err := pcConvergedClient()
	if err != nil {
		return err
	}

	spec := subnetModels.NewIpUnreserveSpec()
	unreserveType := subnetModels.UNRESERVETYPE_CONTEXT
	spec.UnreserveType = &unreserveType
	spec.ClientContext = ptr.To(clientContext)

	taskRef, err := client.Subnets.UnreserveIpsBySubnetId(convergedV4.WithRequestID(ctx, requestID), subnetExtID, spec)
	if err != nil {
		if isNotFoundErr(err) {
			return nil
		}
		return fmt.Errorf("unreserve IP on subnet %s: %w", subnetExtID, err)
	}
	taskID, err := taskExtID(taskRef)
	if err != nil {
		return fmt.Errorf("unreserve IP on subnet %s: %w", subnetExtID, err)
	}
	if err := waitIPTask(ctx, client, taskID); err != nil {
		if isNotFoundErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// reservedIPsByContext lists reserved IPv4 addresses on subnetExtID whose clientContext matches.
func reservedIPsByContext(ctx context.Context, subnetExtID, clientContext string) ([]string, error) {
	client, err := pcConvergedClient()
	if err != nil {
		return nil, err
	}
	raw, err := client.Subnets.ListReservedIpsBySubnetId(ctx, subnetExtID)
	if err != nil {
		return nil, fmt.Errorf("list reserved IPs on subnet %s: %w", subnetExtID, err)
	}

	entries, err := decodeReservedIPs(raw)
	if err != nil {
		return nil, fmt.Errorf("decode reserved IPs on subnet %s: %w", subnetExtID, err)
	}

	ips := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ClientContext == nil || *entry.ClientContext != clientContext {
			continue
		}
		if entry.Ipv4Address == nil || *entry.Ipv4Address == "" {
			continue
		}
		ips = append(ips, *entry.Ipv4Address)
	}
	return ips, nil
}

func pcConvergedClient() (*convergedV4.Client, error) {
	// The rest of the e2e suite uses NUTANIX_USER (matching test/e2e/config/nutanix.yaml and the
	// CCM's own config rendering); prism-go-client's local provider expects NUTANIX_USERNAME.
	if os.Getenv("NUTANIX_USERNAME") == "" {
		if err := os.Setenv("NUTANIX_USERNAME", strings.TrimSpace(os.Getenv("NUTANIX_USER"))); err != nil {
			return nil, fmt.Errorf("failed to set NUTANIX_USERNAME: %w", err)
		}
	}

	params := pcClientParams{env: environment.NewEnvironment(local.NewProvider())}
	return convergedV4.NewClientCache().GetOrCreate(params)
}

func taskExtID(taskRef *networkingprism.TaskReference) (string, error) {
	if taskRef == nil || taskRef.ExtId == nil || *taskRef.ExtId == "" {
		return "", fmt.Errorf("empty task reference")
	}
	return *taskRef.ExtId, nil
}

func waitIPTask(ctx context.Context, client *convergedV4.Client, taskID string) error {
	if client == nil || client.Tasks == nil {
		return fmt.Errorf("prism task client is not initialized")
	}
	return wait.PollUntilContextTimeout(ctx, ipTaskPollInterval, ipTaskWaitTimeout, true, func(ctx context.Context) (bool, error) {
		task, err := client.Tasks.Get(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("get task %s: %w", taskID, err)
		}
		if task == nil || task.Status == nil {
			return false, nil
		}
		switch *task.Status {
		case prismModels.TASKSTATUS_SUCCEEDED:
			return true, nil
		case prismModels.TASKSTATUS_FAILED, prismModels.TASKSTATUS_CANCELED, prismModels.TASKSTATUS_CANCELING:
			return false, fmt.Errorf("task %s finished with status %s", taskID, task.Status.GetName())
		default:
			return false, nil
		}
	})
}

func decodeReservedIPs(raw any) ([]subnetModels.ReservedIp, error) {
	switch t := raw.(type) {
	case *subnetModels.ListSubnetReservedIpsApiResponse:
		if t == nil {
			return nil, nil
		}
		return reservedIPSlice(t.GetData())
	case subnetModels.ListSubnetReservedIpsApiResponse:
		return reservedIPSlice(t.GetData())
	case []subnetModels.ReservedIp:
		return t, nil
	case nil:
		return nil, nil
	default:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("unsupported reserved-IP response type %T: %w", raw, err)
		}
		var resp subnetModels.ListSubnetReservedIpsApiResponse
		if err := json.Unmarshal(encoded, &resp); err == nil {
			return reservedIPSlice(resp.GetData())
		}
		var list []subnetModels.ReservedIp
		if err := json.Unmarshal(encoded, &list); err == nil {
			return list, nil
		}
		return nil, fmt.Errorf("unsupported reserved-IP response type %T", raw)
	}
}

func reservedIPSlice(data any) ([]subnetModels.ReservedIp, error) {
	switch t := data.(type) {
	case nil:
		return nil, nil
	case []subnetModels.ReservedIp:
		return t, nil
	case []any:
		out := make([]subnetModels.ReservedIp, 0, len(t))
		for _, item := range t {
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			var entry subnetModels.ReservedIp
			if err := json.Unmarshal(encoded, &entry); err != nil {
				return nil, err
			}
			out = append(out, entry)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected reserved-IP data type %T", data)
	}
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *converged.APIError
	if errors.As(err, &apiErr) && apiErr.Kind == converged.ErrNotFound {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
