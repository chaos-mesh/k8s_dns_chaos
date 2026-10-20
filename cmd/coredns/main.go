package main

import (
	_ "github.com/chaos-mesh/k8s_dns_chaos/chaos"

	"github.com/coredns/coredns/core/dnsserver"
	_ "github.com/coredns/coredns/core/plugin"
	"github.com/coredns/coredns/coremain"
)

func init() {
	directives := make([]string, 0, len(dnsserver.Directives)+1)
	for _, directive := range dnsserver.Directives {
		if directive == "cache" {
			directives = append(directives, "dns_chaos")
		}
		directives = append(directives, directive)
	}
	dnsserver.Directives = directives
}

func main() {
	coremain.Run()
}
