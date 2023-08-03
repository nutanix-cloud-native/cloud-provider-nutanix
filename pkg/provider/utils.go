package provider

import (
	"fmt"
	"os"
	"time"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/internal/constants"
)

// GetCCMNamespace returns the CCM controller pod namespace
func GetCCMNamespace() (string, error) {
	ns := os.Getenv(constants.CCMNamespaceKey)
	if ns == "" {
		return "", fmt.Errorf("failed to retrieve CCM namespace. Make sure %s env variable is set", constants.CCMNamespaceKey)
	}
	return ns, nil
}

// NoResyncPeriodFunc returns the 0 resync period
func NoResyncPeriodFunc() time.Duration {
	return 0
}
