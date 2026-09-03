//go:build integration

package integration

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/nutanix-cloud-native/prism-go-client/environment/types"
)

// NUTANIX_E2E_* env vars align with prism-go-client tests/e2e.
const (
	envNutanixE2EEndpoint = "NUTANIX_E2E_ENDPOINT"
	envNutanixE2EPort     = "NUTANIX_E2E_PORT"
	envNutanixE2EUsername = "NUTANIX_E2E_USERNAME"
	envNutanixE2EPassword = "NUTANIX_E2E_PASSWORD"
	envNutanixE2EInsecure = "NUTANIX_E2E_INSECURE"
)

func requiredNutanixE2EEnvVars() []string {
	return []string{
		envNutanixE2EEndpoint,
		envNutanixE2EPort,
		envNutanixE2EUsername,
		envNutanixE2EPassword,
		envNutanixE2EInsecure,
	}
}

func requireE2EEnv(t testing.TB, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf(
			"missing required environment variable %q; set NUTANIX_E2E_* (see Makefile test-integration); required: %v",
			name,
			requiredNutanixE2EEnvVars(),
		)
	}
	return value
}

func managementEndpointFromE2EEnv(t testing.TB) types.ManagementEndpoint {
	t.Helper()

	endpoint := requireE2EEnv(t, envNutanixE2EEndpoint)
	port := requireE2EEnv(t, envNutanixE2EPort)
	username := requireE2EEnv(t, envNutanixE2EUsername)
	password := requireE2EEnv(t, envNutanixE2EPassword)
	insecureRaw := requireE2EEnv(t, envNutanixE2EInsecure)

	insecure, err := strconv.ParseBool(insecureRaw)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", envNutanixE2EInsecure, insecureRaw, err)
	}

	address, err := url.Parse(fmt.Sprintf("https://%s:%s", endpoint, port))
	if err != nil {
		t.Fatalf("invalid e2e endpoint URL from %s/%s: %v", envNutanixE2EEndpoint, envNutanixE2EPort, err)
	}

	return types.ManagementEndpoint{
		Address: address,
		ApiCredentials: types.ApiCredentials{
			Username: username,
			Password: password,
		},
		Insecure: insecure,
	}
}
