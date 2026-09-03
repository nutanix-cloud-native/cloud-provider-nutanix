//go:build integration

// Integration tests for CCM RetryOnStale adoption against a real Prism Central.
//
// These exercise the same converged operations CCM wraps (Clusters.List / Get)
// under RetryOnStale, using helpers.StripSDKAuthorization to force the
// NCN-116866 stale-session failure mode without waiting for IAM expiry.
//
// Required env vars:
//
//	NUTANIX_E2E_ENDPOINT, NUTANIX_E2E_PORT, NUTANIX_E2E_USERNAME,
//	NUTANIX_E2E_PASSWORD, NUTANIX_E2E_INSECURE
//
// Run:
//
//	make test-integration
package integration

import (
	"context"
	"testing"

	"github.com/nutanix-cloud-native/prism-go-client/converged"
	convergedV4 "github.com/nutanix-cloud-native/prism-go-client/converged/v4"
	"github.com/nutanix-cloud-native/prism-go-client/environment/types"
	"github.com/nutanix-cloud-native/prism-go-client/tests/e2e/helpers"
	clusterModels "github.com/nutanix/ntnx-api-golang-clients/clustermgmt-go-client/v4/models/clustermgmt/v4/config"
)

type cachedClientParams struct {
	name         string
	mgmtEndpoint types.ManagementEndpoint
}

func (c *cachedClientParams) Key() string { return c.name }

func (c *cachedClientParams) ManagementEndpoint() types.ManagementEndpoint {
	return c.mgmtEndpoint
}

func newLiveCachedClient(t *testing.T, cacheKey string) (*convergedV4.ClientCache, *cachedClientParams, *convergedV4.Client) {
	t.Helper()
	mgmtEndpoint := managementEndpointFromE2EEnv(t)
	cache := convergedV4.NewClientCache()
	cp := &cachedClientParams{
		name:         cacheKey,
		mgmtEndpoint: mgmtEndpoint,
	}
	client, err := cache.GetOrCreate(cp)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if client == nil {
		t.Fatal("GetOrCreate returned nil client")
	}

	ctx := context.Background()
	_, err = client.Clusters.List(ctx, converged.WithLimit(1))
	if err != nil {
		t.Fatalf("sanity Clusters.List must succeed with e2e credentials: %v", err)
	}
	return cache, cp, client
}

func TestRetryOnStaleListClusters(t *testing.T) {
	_, _, client := newLiveCachedClient(t, "ccm-integration-retry-on-stale-list")
	ctx := context.Background()

	clusters, err := convergedV4.RetryOnStale(client, func(c *convergedV4.Client) ([]clusterModels.Cluster, error) {
		return c.Clusters.List(ctx, converged.WithLimit(1))
	})
	if err != nil {
		t.Fatalf("RetryOnStale Clusters.List: %v", err)
	}
	if len(clusters) == 0 {
		t.Fatal("expected at least one cluster from live PC")
	}
}

func TestRetryOnStaleRecoversAfterStripSDKAuthorization(t *testing.T) {
	cache, cp, poisoned := newLiveCachedClient(t, "ccm-integration-retry-on-stale-recover")
	ctx := context.Background()

	helpers.StripSDKAuthorization(poisoned)
	_, err := poisoned.Clusters.List(ctx, converged.WithLimit(1))
	if err == nil {
		t.Fatal("expected error after StripSDKAuthorization")
	}
	if !converged.IsStaleClient(err) {
		t.Fatalf("expected IsStaleClient after StripSDKAuthorization; got: %v", err)
	}

	calls := 0
	var recovered *convergedV4.Client
	_, err = convergedV4.RetryOnStale(poisoned, func(c *convergedV4.Client) ([]clusterModels.Cluster, error) {
		calls++
		recovered = c
		if calls == 1 {
			helpers.StripSDKAuthorization(c)
		}
		return c.Clusters.List(ctx, converged.WithLimit(1))
	})
	if err != nil {
		t.Fatalf("RetryOnStale after poison: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected RetryOnStale to invoke fn twice, got %d", calls)
	}
	if recovered == nil {
		t.Fatal("expected recovered client")
	}
	if recovered == poisoned {
		t.Fatal("expected recovered client to differ from poisoned instance")
	}

	cached, err := cache.GetOrCreate(cp)
	if err != nil {
		t.Fatalf("GetOrCreate after refresh: %v", err)
	}
	if cached == poisoned {
		t.Fatal("cache must not still return the poisoned client")
	}
	if cached != recovered {
		t.Fatal("cache should return the recovered client")
	}

	// Also exercise Get on a known UUID under RetryOnStale (CCM GetCluster path).
	clusters, err := convergedV4.RetryOnStale(cached, func(c *convergedV4.Client) ([]clusterModels.Cluster, error) {
		return c.Clusters.List(ctx, converged.WithLimit(1))
	})
	if err != nil {
		t.Fatalf("list for Get UUID: %v", err)
	}
	if len(clusters) == 0 || clusters[0].ExtId == nil || *clusters[0].ExtId == "" {
		t.Fatal("expected cluster with ExtId")
	}
	clusterUUID := *clusters[0].ExtId
	_, err = convergedV4.RetryOnStale(cached, func(c *convergedV4.Client) (*clusterModels.Cluster, error) {
		return c.Clusters.Get(ctx, clusterUUID)
	})
	if err != nil {
		t.Fatalf("RetryOnStale Clusters.Get(%s): %v", clusterUUID, err)
	}
}

func TestPoisonWithoutRetryOnStaleRemainsStale(t *testing.T) {
	cache, cp, poisoned := newLiveCachedClient(t, "ccm-integration-no-retry-remains-stale")
	ctx := context.Background()

	helpers.StripSDKAuthorization(poisoned)
	_, err := poisoned.Clusters.List(ctx, converged.WithLimit(1))
	if err == nil || !converged.IsStaleClient(err) {
		t.Fatalf("expected IsStaleClient after StripSDKAuthorization; got: %v", err)
	}

	_, err = poisoned.Clusters.List(ctx, converged.WithLimit(1))
	if err == nil || !converged.IsStaleClient(err) {
		t.Fatalf("without RetryOnStale, subsequent List must stay stale; got: %v", err)
	}

	cached, err := cache.GetOrCreate(cp)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if cached != poisoned {
		t.Fatal("poisoned client must remain cached without RetryOnStale/Invalidate")
	}
	_, err = cached.Clusters.List(ctx, converged.WithLimit(1))
	if err == nil || !converged.IsStaleClient(err) {
		t.Fatalf("cached client must still be stale; got: %v", err)
	}
}
