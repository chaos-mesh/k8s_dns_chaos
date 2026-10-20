package main

import (
	"slices"
	"testing"

	"github.com/coredns/coredns/core/dnsserver"
)

func TestDNSChaosPrecedesCacheAndKubernetes(t *testing.T) {
	chaosIndex := slices.Index(dnsserver.Directives, "dns_chaos")
	cacheIndex := slices.Index(dnsserver.Directives, "cache")
	kubernetesIndex := slices.Index(dnsserver.Directives, "kubernetes")
	if chaosIndex < 0 || cacheIndex != chaosIndex+1 {
		t.Fatal("dns_chaos must immediately precede cache")
	}
	if kubernetesIndex <= cacheIndex {
		t.Fatal("dns_chaos and cache must precede kubernetes")
	}
}
