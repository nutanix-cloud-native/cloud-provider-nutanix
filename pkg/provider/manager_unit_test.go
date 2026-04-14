package provider

import (
	"context"
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	vmmCommonModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/common/v1/config"
	vmmModels "github.com/nutanix/ntnx-api-golang-clients/vmm-go-client/v4/models/vmm/v4/ahv/config"
	"go4.org/netipx"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/nutanix-cloud-native/cloud-provider-nutanix/pkg/provider/config"
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

func TestGetNodeAddresses(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		vm            *vmmModels.Vm
		wantErr       bool
		wantAddresses []v1.NodeAddress
	}{
		{
			name: "VM with no NICs returns error",
			vm: vmWithNICS(
				t,
				"my-vm",
				"uuid-1",
				[]vmmModels.Nic{},
			),
			wantErr: true,
		},
		{
			name: "VM with one NIC returns internal IP and hostname",
			vm: vmWithNICS(
				t,
				"my-vm",
				"uuid-1",
				[]vmmModels.Nic{
					nicWithIPs(t, "10.0.0.1", []string{"10.0.0.2"}, []string{"10.0.0.3"}),
				},
			),
			wantAddresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.1",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.2",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.3",
				},
				{
					Type:    v1.NodeHostName,
					Address: "my-vm",
				},
			},
		},
		{
			name: "VM with two NICs, each with different internal IPs: addresses appear in NIC order",
			vm: vmWithNICS(
				t,
				"my-vm",
				"uuid-2",
				[]vmmModels.Nic{
					nicWithIPs(t, "10.0.0.1", []string{"10.0.0.2", "10.0.0.3"}, []string{"10.0.0.4"}),
					nicWithIPs(t, "10.0.0.5", []string{"10.0.0.6", "10.0.0.7"}, []string{"10.0.0.8"}),
				}),
			wantAddresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.1",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.2",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.3",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.4",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.5",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.6",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.7",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.8",
				},
				{
					Type:    v1.NodeHostName,
					Address: "my-vm",
				},
			},
		},
		{
			name: "VM with two NICs, each with the same internal IP: the internal IP appears once",
			vm: vmWithNICS(
				t,
				"my-vm",
				"uuid-3",
				[]vmmModels.Nic{
					nicWithIPs(t, "10.0.0.1", []string{"10.0.0.2"}, []string{"10.0.0.3"}),
					nicWithIPs(t, "10.0.0.1", []string{"10.0.0.4"}, []string{"10.0.0.5"}),
				}),
			wantAddresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.1",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.2",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.3",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.4",
				},
				{
					Type:    v1.NodeInternalIP,
					Address: "10.0.0.5",
				},
				{
					Type:    v1.NodeHostName,
					Address: "my-vm",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &nutanixManager{
				// We have to initialize ignoredNodeIPs, or the test will fail with a nil pointer dereference.
				ignoredNodeIPs: ignoredIPSet("10.0.0.99"),
			}
			gotAddresses, err := m.getNodeAddresses(ctx, tt.vm)
			if tt.wantErr {
				if err == nil {
					t.Errorf("getNodeAddresses() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("getNodeAddresses() err = %v", err)
				return
			}

			if diff := cmp.Diff(tt.wantAddresses, gotAddresses); diff != "" {
				t.Errorf("getNodeAddresses() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func ignoredIPSet(ips ...string) *netipx.IPSet {
	b := netipx.IPSetBuilder{}
	for _, s := range ips {
		b.Add(netip.MustParseAddr(s))
	}
	s, _ := b.IPSet()
	return s
}

// nicWithIP returns a NIC with the given IPv4 address.
func nicWithIPs(t *testing.T, primaryIP string, secondaryIPs []string, learnedIPs []string) vmmModels.Nic {
	t.Helper()
	netInfo := vmmModels.NewVirtualEthernetNicNetworkInfo()

	netInfo.Ipv4Config = vmmModels.NewIpv4Config()
	netInfo.Ipv4Config.IpAddress = &vmmCommonModels.IPv4Address{Value: ptr.To(primaryIP)}
	netInfo.Ipv4Config.SecondaryIpAddressList = make([]vmmCommonModels.IPv4Address, 0, len(secondaryIPs))
	for _, secondaryIP := range secondaryIPs {
		netInfo.Ipv4Config.SecondaryIpAddressList = append(
			netInfo.Ipv4Config.SecondaryIpAddressList,
			vmmCommonModels.IPv4Address{Value: ptr.To(secondaryIP)},
		)
	}

	netInfo.Ipv4Info = vmmModels.NewIpv4Info()
	netInfo.Ipv4Info.LearnedIpAddresses = make([]vmmCommonModels.IPv4Address, 0, len(learnedIPs))
	for _, learnedIP := range learnedIPs {
		netInfo.Ipv4Info.LearnedIpAddresses = append(
			netInfo.Ipv4Info.LearnedIpAddresses,
			vmmCommonModels.IPv4Address{Value: ptr.To(learnedIP)},
		)
	}

	nic := vmmModels.NewNic()
	if err := nic.SetNicNetworkInfo(*netInfo); err != nil {
		t.Fatalf("SetNicNetworkInfo: %v", err)
	}
	return *nic
}

// vmWithNICS returns a VM with the given NICs.
func vmWithNICS(t *testing.T, name, uuid string, nics []vmmModels.Nic) *vmmModels.Vm {
	t.Helper()
	return &vmmModels.Vm{
		ExtId: ptr.To(uuid),
		Name:  ptr.To(name),
		Nics:  nics,
	}
}

func TestNodeMatchesSelector(t *testing.T) {
	tests := []struct {
		name         string
		nodeLabels   map[string]string
		nodeSelector *metav1.LabelSelector
		want         bool
		wantErr      bool
	}{
		{
			name:         "nil selector matches all nodes",
			nodeLabels:   map[string]string{"foo": "bar"},
			nodeSelector: nil,
			want:         true,
		},
		{
			name:       "matchLabels: node matches",
			nodeLabels: map[string]string{"role": "worker"},
			nodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "worker"},
			},
			want: true,
		},
		{
			name:       "matchLabels: node does not match",
			nodeLabels: map[string]string{"role": "control-plane"},
			nodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "worker"},
			},
			want: false,
		},
		{
			name:       "matchExpressions In: node matches",
			nodeLabels: map[string]string{"zone": "us-east-1a"},
			nodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "zone",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"us-east-1a", "us-east-1b"},
				}},
			},
			want: true,
		},
		{
			name:       "matchExpressions NotIn: node is excluded",
			nodeLabels: map[string]string{"zone": "us-east-1a"},
			nodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "zone",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"us-east-1a"},
				}},
			},
			want: false,
		},
		{
			name:       "matchExpressions NotIn: node without the label matches",
			nodeLabels: map[string]string{},
			nodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "zone",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"us-east-1a"},
				}},
			},
			want: true,
		},
		{
			name:       "matchLabels + matchExpressions NotIn: both must hold",
			nodeLabels: map[string]string{"role": "worker", "zone": "us-east-1a"},
			nodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "worker"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "zone",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"us-east-1a"},
				}},
			},
			want: false, // NotIn fails even though matchLabels passes
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &nutanixManager{
				config: config.Config{NodeSelector: tt.nodeSelector},
			}
			node := &v1.Node{}
			node.Labels = tt.nodeLabels
			got, err := m.nodeMatchesSelector(node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("nodeMatchesSelector() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("nodeMatchesSelector() = %v, want %v", got, tt.want)
			}
		})
	}
}
