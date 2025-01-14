package provider

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestIsNodeAddressesSet(t *testing.T) {
	tests := []struct {
		name string
		node *v1.Node
		want bool
	}{
		{
			name: "found an internalIP and a hostname",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeHostName,
							Address: "example.com",
						},
						{
							Type:    v1.NodeInternalIP,
							Address: "1.2.3.4",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "found only internalIPs",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeInternalIP,
							Address: "1.2.3.4",
						},
						{
							Type:    v1.NodeInternalIP,
							Address: "5.6.7.8",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "found only a hostname",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeHostName,
							Address: "example.com",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "found no addresses",
			node: &v1.Node{
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &nutanixManager{}
			if got := n.isNodeAddressesSet(tt.node); got != tt.want {
				t.Errorf("nutanixManager.isNodeAddressesSet() = %v, want %v", got, tt.want)
			}
		})
	}
}
