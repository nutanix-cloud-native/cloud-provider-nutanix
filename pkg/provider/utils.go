package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

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

// SanitizeK8sLabelValue converts the input string into a valid Kubernetes label value.
// Returns a valid value that can be set as a K8s label value.
func SanitizeK8sLabelValue(s string) string {
	if s == "" {
		return s
	}

	// K8s label value must meet the reqex `^[a-zA-Z0-9]([a-zA-Z0-9\-_.]*[a-zA-Z0-9])?$`
	// and string length cannot exceed 63.
	// Replace all invalid characters with underscore
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			// Replace everything else (spaces, slashes, quotes, etc.)
			b.WriteRune('_')
		}
	}

	cleaned := b.String()

	// Collapse consecutive invalid chars into a single underscore
	cleaned = regexp.MustCompile(`_+`).ReplaceAllString(cleaned, "_")

	// Trim leading/trailing underscores (and other separators)
	cleaned = strings.Trim(cleaned, "-_.")

	// Enforce max length 63
	if len(cleaned) > 63 {
		cleaned = cleaned[:63]
		// After truncation, trim trailing invalid chars again
		cleaned = strings.TrimRight(cleaned, "-_.")
	}

	return cleaned
}
