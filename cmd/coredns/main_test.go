package main

import (
	"testing"

	"github.com/coredns/coredns/core/dnsserver"
)

func TestDNSChaosPrecedesKubernetes(t *testing.T) {
	for index, directive := range dnsserver.Directives {
		if directive != "dns_chaos" {
			continue
		}

		if index+1 >= len(dnsserver.Directives) || dnsserver.Directives[index+1] != "kubernetes" {
			t.Fatal("dns_chaos must immediately precede kubernetes")
		}
		return
	}

	t.Fatal("dns_chaos directive is not registered")
}
