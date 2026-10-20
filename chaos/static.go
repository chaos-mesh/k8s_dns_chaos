package chaos

import (
	"net"
	"net/netip"
	"strings"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/miekg/dns"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/validation"
)

func parseStaticMappings(req *pb.SetDNSChaosRequest) (map[string]netip.Addr, error) {
	if req.Action != ActionStatic {
		return nil, nil
	}
	if len(req.IpDomainMaps) == 0 {
		return nil, status.Error(codes.InvalidArgument, "static action requires at least one domain-to-IP mapping")
	}
	ips := make(map[string]netip.Addr, len(req.IpDomainMaps))
	for _, mapping := range req.IpDomainMaps {
		if mapping == nil {
			return nil, status.Error(codes.InvalidArgument, "static action contains an empty mapping")
		}
		domain := strings.TrimSuffix(strings.ToLower(mapping.Domain), ".")
		if errs := validation.IsDNS1123Subdomain(domain); len(errs) != 0 {
			return nil, status.Errorf(codes.InvalidArgument, "invalid static domain %q: %s", mapping.Domain, strings.Join(errs, "; "))
		}
		for _, label := range strings.Split(domain, ".") {
			if len(label) > 63 {
				return nil, status.Errorf(codes.InvalidArgument, "static domain %q contains a label longer than 63 characters", mapping.Domain)
			}
		}
		ip, err := netip.ParseAddr(mapping.Ip)
		if err != nil || ip.Zone() != "" {
			return nil, status.Errorf(codes.InvalidArgument, "invalid static IP %q", mapping.Ip)
		}
		if _, exists := ips[domain]; exists {
			return nil, status.Errorf(codes.InvalidArgument, "duplicate static domain %q", domain)
		}
		ips[domain] = ip.Unmap()
	}
	return ips, nil
}

func staticAnswers(ips map[string]netip.Addr, qname string, qtype uint16) ([]dns.RR, bool) {
	ip, ok := ips[normalizeDomain(qname)]
	if !ok {
		return nil, false
	}
	switch qtype {
	case dns.TypeA:
		if ip.Is4() {
			return a(qname, 10, []net.IP{ip.AsSlice()}), true
		}
	case dns.TypeAAAA:
		if ip.Is6() {
			return aaaa(qname, 10, []net.IP{ip.AsSlice()}), true
		}
	default:
		return nil, false
	}
	return nil, true
}
