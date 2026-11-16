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
	"reflect"
	"testing"

	subnetModels "github.com/nutanix/ntnx-api-golang-clients/networking-go-client/v4/models/networking/v4/config"
	"k8s.io/utils/ptr"
)

const (
	peA = "00065e57-0f1b-17bc-289f-7cc25586eef1"
	peB = "fc3b713a-e5cc-469c-954e-25ed36c7ddcd"
)

func subnet(name, extID, clusterRef string, clusterRefs ...string) subnetModels.Subnet {
	s := subnetModels.Subnet{ClusterReferenceList: clusterRefs}
	if name != "" {
		s.Name = ptr.To(name)
	}
	if extID != "" {
		s.ExtId = ptr.To(extID)
	}
	if clusterRef != "" {
		s.ClusterReference = ptr.To(clusterRef)
	}
	return s
}

func Test_matchSubnetsByName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		subnets      []subnetModels.Subnet
		want         string
		clusterExtID string
		expect       []string
	}{
		{
			name:    "exact name",
			subnets: []subnetModels.Subnet{subnet("vlan170", "s-1", peA), subnet("vlan171", "s-2", peA)},
			want:    "vlan170",
			expect:  []string{"s-1"},
		},
		{
			// Prism rejects a filter holding non-ASCII characters, so these names
			// are matched here, over the full subnet list. Case folding would
			// collapse two real, distinct subnets into one.
			name:    "unicode case folding is not applied",
			subnets: []subnetModels.Subnet{subnet("aßa", "s-1", peA), subnet("aẞa", "s-2", peA)},
			want:    "aßa",
			expect:  []string{"s-1"},
		},
		{
			name:    "ascii case is significant",
			subnets: []subnetModels.Subnet{subnet("nkp_managed", "s-1", peA)},
			want:    "NKP_MANAGED",
			expect:  []string{},
		},
		{
			name:         "cluster narrows a duplicated name",
			subnets:      []subnetModels.Subnet{subnet("shared", "s-1", peA), subnet("shared", "s-2", peB)},
			want:         "shared",
			clusterExtID: peB,
			expect:       []string{"s-2"},
		},
		{
			name:    "a duplicated name without a cluster stays ambiguous",
			subnets: []subnetModels.Subnet{subnet("shared", "s-1", peA), subnet("shared", "s-2", peB)},
			want:    "shared",
			expect:  []string{"s-1", "s-2"},
		},
		{
			name:         "cluster matches the reference list",
			subnets:      []subnetModels.Subnet{subnet("vlan170", "s-1", "", peB, peA)},
			want:         "vlan170",
			clusterExtID: peA,
			expect:       []string{"s-1"},
		},
		{
			// An overlay subnet declares no cluster. CAREN fills the cluster in
			// whenever a name is used, so narrowing it away would make every
			// overlay subnet unreachable by name.
			name:         "a subnet with no cluster is not narrowed away",
			subnets:      []subnetModels.Subnet{subnet("overlay", "s-1", "")},
			want:         "overlay",
			clusterExtID: peA,
			expect:       []string{"s-1"},
		},
		{
			name:         "a cluster-scoped subnet elsewhere is excluded",
			subnets:      []subnetModels.Subnet{subnet("vlan170", "s-1", peA)},
			want:         "vlan170",
			clusterExtID: peB,
			expect:       []string{},
		},
		{
			name:    "a subnet with no extID is skipped",
			subnets: []subnetModels.Subnet{subnet("vlan170", "", peA)},
			want:    "vlan170",
			expect:  []string{},
		},
		{
			name:    "a subnet with no name is skipped",
			subnets: []subnetModels.Subnet{subnet("", "s-1", peA)},
			want:    "vlan170",
			expect:  []string{},
		},
		{
			name:    "no subnets",
			subnets: nil,
			want:    "vlan170",
			expect:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchSubnetsByName(tt.subnets, tt.want, tt.clusterExtID)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Fatalf("matchSubnetsByName() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func Test_isASCII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  string
		expect bool
	}{
		{value: "vlan170", expect: true},
		{value: "", expect: true},
		{value: "a-b_c.1 2", expect: true},
		{value: "o'brien", expect: true},
		{value: "aßa", expect: false},
		{value: "aẞa", expect: false},
		{value: "🏦", expect: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			if got := isASCII(tt.value); got != tt.expect {
				t.Fatalf("isASCII(%q) = %v, want %v", tt.value, got, tt.expect)
			}
		})
	}
}

func Test_subnetClusterPredicates(t *testing.T) {
	t.Parallel()

	withRef := subnet("a", "s-1", peA)
	withList := subnet("a", "s-1", "", peA)
	withNone := subnet("a", "s-1", "")
	withEmptyRef := subnetModels.Subnet{Name: ptr.To("a"), ExtId: ptr.To("s-1"), ClusterReference: ptr.To("")}

	if !subnetDeclaresCluster(&withRef) || !subnetDeclaresCluster(&withList) {
		t.Fatal("a subnet with a cluster reference must declare a cluster")
	}
	if subnetDeclaresCluster(&withNone) || subnetDeclaresCluster(&withEmptyRef) {
		t.Fatal("a subnet with no cluster reference must not declare a cluster")
	}
	if !subnetInCluster(&withRef, peA) || !subnetInCluster(&withList, peA) {
		t.Fatal("a subnet must match its own cluster")
	}
	if subnetInCluster(&withRef, peB) || subnetInCluster(&withNone, peA) {
		t.Fatal("a subnet must not match another cluster")
	}
}
