//go:build e2e

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/cluster-api/test/e2e"
)

var _ = Describe("CCM on a CAPX Cluster", Label("capx"), func() {
	e2e.QuickStartSpec(ctx, func() e2e.QuickStartSpecInput {
		return e2e.QuickStartSpecInput{
			E2EConfig:             e2eConfig,
			ClusterctlConfigPath:  clusterctlConfigPath,
			BootstrapClusterProxy: bootstrapClusterProxy,
			ArtifactFolder:        artifactFolder,
			SkipCleanup:           skipCleanup,
		}
	})
})
