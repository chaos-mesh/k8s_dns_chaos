package kubernetes

import (
	"reflect"
	"testing"

	"github.com/coredns/caddy"
)

func TestKubernetesParseTransfer(t *testing.T) {
	tests := []struct {
		name      string
		directive string
		expected  []string
		shouldErr bool
	}{
		{"default port", "transfer to 1.2.3.4", []string{"1.2.3.4:53"}, false},
		{"explicit port", "transfer to 1.2.3.4:5353", []string{"1.2.3.4:5353"}, false},
		{"ipv6", "transfer to 2001:db8::1 [2001:db8::2]:5353", []string{"[2001:db8::1]:53", "[2001:db8::2]:5353"}, false},
		{"mixed targets", "transfer to * 1.2.3.4", []string{"*", "1.2.3.4:53"}, false},
		{"wildcard", "transfer to *", []string{"*"}, false},
		{"empty targets", "transfer to", nil, false},
		{"unknown direction", "transfer unknown", nil, false},
		{"missing direction", "transfer", nil, true},
		{"inbound", "transfer from 1.2.3.4", nil, true},
		{"invalid address", "transfer to example.com", nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", "k8s_dns_chaos cluster.local {\n"+tc.directive+"\n}")
			k, err := kubernetesParse(c)
			if tc.shouldErr {
				if err == nil {
					t.Fatal("expected a configuration error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(k.TransferTo, tc.expected) {
				t.Fatalf("transfer destinations = %v, want %v", k.TransferTo, tc.expected)
			}
		})
	}
}
