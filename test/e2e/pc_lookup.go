//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nutanix-cloud-native/prism-go-client/converged"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment"
	"github.com/nutanix-cloud-native/prism-go-client/environment/providers/local"
	envtypes "github.com/nutanix-cloud-native/prism-go-client/environment/types"
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
