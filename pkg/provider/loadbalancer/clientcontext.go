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
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// clientContext is the only identity Prism persists with a reservation, so it
// has to carry both the cluster and the Service. Kubernetes annotations and
// pool labels are a cache: they are gone once the workload cluster is.
//
// Prism caps clientContext at 64 characters and rejects a longer value
// synchronously with HTTP 400. Two hyphenated UUIDs are 73 characters, so each
// UUID's 16 raw bytes are base64url-encoded to 22 characters instead:
//
//	nkp:<b64u(clusterUID)>:<b64u(serviceUID)>  49 chars  this strategy
//
// The shared prefix nkp:<b64u(clusterUID)>: is what lets a cluster teardown
// release every Service address this cluster holds without the workload
// cluster.
//
// It does NOT cover the API server address. That one is reserved by the Nutanix
// IPAM provider, which writes the IPAddressClaim's UID verbatim as the context
// and offers no way to override it, so it carries no nkp: prefix and has to be
// swept by exact context instead. Measured on PC 7.6 GA; assuming otherwise is
// what once leaked the API server address on every cluster delete.
//
// CAREN's cluster teardown recomputes this prefix with the same encoding
// (pkg/handlers/lifecycle/servicelbgc/reservation.go). The two implementations
// have to agree, so neither may change the grammar alone.
const (
	clientContextDomain = "nkp"
	clientContextSep    = ":"
	clientContextMaxLen = 64
)

// serviceClientContext returns the clientContext for a Service reservation.
func serviceClientContext(clusterUID, serviceUID string) (string, error) {
	if clusterUID == "" {
		return "", fmt.Errorf("clusterUID is required to build a reservation context")
	}
	if serviceUID == "" {
		return "", fmt.Errorf("service UID is required to build a reservation context")
	}
	context := clusterContextPrefix(clusterUID) + encodeUID(serviceUID)
	if len(context) > clientContextMaxLen {
		return "", fmt.Errorf("reservation context %q is %d characters; prism allows %d",
			context, len(context), clientContextMaxLen)
	}
	return context, nil
}

// clusterContextPrefix returns the prefix shared by every Service reservation
// this cluster holds. The API server address does not carry it; see above.
func clusterContextPrefix(clusterUID string) string {
	return clientContextDomain + clientContextSep + encodeUID(clusterUID) + clientContextSep
}

// encodeUID packs a UID into 22 characters. A Kubernetes UID is a UUID, whose
// 16 raw bytes encode losslessly and reversibly. Anything else is hashed to 16
// bytes, which stays unique and within budget at the cost of reversibility,
// rather than failing a reservation over the shape of a UID.
func encodeUID(uid string) string {
	if parsed, err := uuid.Parse(uid); err == nil {
		return base64.RawURLEncoding.EncodeToString(parsed[:])
	}
	sum := sha256.Sum256([]byte(uid))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// isOwnedContext reports whether context was written by this feature. A
// reservation made by anything else is never touched, so a shared subnet
// carrying a VM reservation or another tool's address is left alone.
func isOwnedContext(context string) bool {
	return strings.HasPrefix(context, clientContextDomain+clientContextSep)
}
