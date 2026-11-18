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

package loadbalancer

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The 64-character cap is a hard, synchronous Prism limit: a longer context is
// rejected with HTTP 400 and the reservation never happens. Two hyphenated
// UUIDs would be 73 characters, which is why the UUIDs are packed.
func TestServiceClientContextFitsPrismLimit(t *testing.T) {
	t.Parallel()

	context, err := serviceClientContext(uuid.NewString(), uuid.NewString())
	if err != nil {
		t.Fatalf("serviceClientContext: %v", err)
	}
	if got := len(context); got != 49 {
		t.Fatalf("context %q is %d characters, want 49", context, got)
	}
	if len(context) > clientContextMaxLen {
		t.Fatalf("context %q exceeds the prism limit of %d", context, clientContextMaxLen)
	}
	if !isOwnedContext(context) {
		t.Fatalf("context %q is not recognised as ours", context)
	}
}

// Two UUIDs are two distinct contexts, and the same pair is always the same
// context: a restart has to rebuild the identity Prism already stores.
func TestServiceClientContextIsStableAndDistinct(t *testing.T) {
	t.Parallel()

	clusterA, clusterB := uuid.NewString(), uuid.NewString()
	svcA, svcB := uuid.NewString(), uuid.NewString()

	first := mustContext(t, clusterA, svcA)
	if again := mustContext(t, clusterA, svcA); again != first {
		t.Fatalf("context is not stable: %q then %q", first, again)
	}
	if other := mustContext(t, clusterA, svcB); other == first {
		t.Fatal("two Services in one cluster share a context")
	}
	if other := mustContext(t, clusterB, svcA); other == first {
		t.Fatal("the same Service UID in two clusters shares a context")
	}
}

// The cluster prefix is what a teardown matches on, so every Service in the
// cluster must carry it and no Service of another cluster may.
func TestClusterContextPrefixMatchesOwnClusterOnly(t *testing.T) {
	t.Parallel()

	cluster, other := uuid.NewString(), uuid.NewString()
	prefix := clusterContextPrefix(cluster)

	if got := len(prefix); got != 27 {
		t.Fatalf("prefix %q is %d characters, want 27", prefix, got)
	}
	for range 5 {
		if context := mustContext(t, cluster, uuid.NewString()); !strings.HasPrefix(context, prefix) {
			t.Fatalf("context %q does not carry prefix %q", context, prefix)
		}
	}
	if context := mustContext(t, other, uuid.NewString()); strings.HasPrefix(context, prefix) {
		t.Fatalf("context %q of another cluster matched prefix %q", context, prefix)
	}
	// The control-plane VIP the IPAM provider reserves shares the prefix, so one
	// sweep covers both halves of the feature.
	if !strings.HasPrefix(prefix+"cp", prefix) {
		t.Fatal("the control-plane context does not share the cluster prefix")
	}
}

// A UID that is not a UUID must still produce a usable context rather than
// failing every reservation in the cluster.
func TestServiceClientContextAcceptsNonUUIDUIDs(t *testing.T) {
	t.Parallel()

	context, err := serviceClientContext("not-a-uuid", "also-not-a-uuid")
	if err != nil {
		t.Fatalf("serviceClientContext: %v", err)
	}
	if len(context) != 49 || !isOwnedContext(context) {
		t.Fatalf("context %q is %d characters", context, len(context))
	}
	if other := mustContext(t, "not-a-uuid", "different"); other == context {
		t.Fatal("two different Service UIDs collided")
	}
}

func TestServiceClientContextRequiresBothUIDs(t *testing.T) {
	t.Parallel()

	if _, err := serviceClientContext("", uuid.NewString()); err == nil {
		t.Fatal("an empty clusterUID was accepted")
	}
	if _, err := serviceClientContext(uuid.NewString(), ""); err == nil {
		t.Fatal("an empty service UID was accepted")
	}
}

// A reservation made by anything other than this feature must never match, so a
// shared subnet carrying VM or third-party reservations is left alone.
func TestIsOwnedContextRejectsForeignReservations(t *testing.T) {
	t.Parallel()

	for _, context := range []string{
		"",
		"some-vm-reservation",
		"terraform",
		"NKP:abc:def",
		"nkp-but-not-ours",
		uuid.NewString(),
	} {
		if isOwnedContext(context) {
			t.Fatalf("foreign context %q was claimed as ours", context)
		}
	}
}

func mustContext(t *testing.T, clusterUID, serviceUID string) string {
	t.Helper()
	context, err := serviceClientContext(clusterUID, serviceUID)
	if err != nil {
		t.Fatalf("serviceClientContext(%q, %q): %v", clusterUID, serviceUID, err)
	}
	return context
}
