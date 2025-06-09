package kubernetes

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// ServeDNS implements the plugin.Handler interface.
func (k Kubernetes) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	sourceIP := state.IP()
	log.Debugf("k8s ServeDNS, source IP: %s, state: %v", sourceIP, state)

	chaosPod, err := k.getChaosPod(sourceIP)
	if err != nil {
		log.Infof("fail to get pod information from cluster, IP: %s, error: %v", sourceIP, err)
	}

	records, extra, zone, err := k.getRecords(ctx, state)
	log.Debugf("records: %v, err: %v", records, err)
	if k.needChaos(chaosPod, records, state.QName()) && chaosPod.Action != ActionStatic {
		return k.chaosDNS(ctx, w, r, state, chaosPod)
	}
	// Check if chaos testing is needed and the action type is static IP.
	if k.needChaos(chaosPod, records, state.QName()) && chaosPod.Action == ActionStatic {
		log.Debugf("need chaos, but action is static")
		// Get the domain-IP mapping for the specific namespace and pod name.
		domainMap := k.domainIPMapByNamespacedName[chaosPod.Namespace][chaosPod.Name]
		// Check if the domain-IP mapping exists.
		if domainMap != nil {
			// Check if the requested domain exists in the mapping.
			if _, ok := domainMap[state.Name()]; ok {
				// Generate DNS records using the domain-IP mapping and return the result.
				return generateDNSRecords(state, domainMap, r, w)
			}
		}
	}

	if k.IsNameError(err) {
		if len(zone) == 0 {
			return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
		}

		if k.Fall.Through(state.Name()) {
			return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
		}
		if !k.APIConn.HasSynced() {
			// If we haven't synchronized with the kubernetes cluster, return server failure
			return plugin.BackendError(ctx, &k, zone, dns.RcodeServerFailure, state, nil /* err */, plugin.Options{})
		}
		return plugin.BackendError(ctx, &k, zone, dns.RcodeNameError, state, nil /* err */, plugin.Options{})
	}
	if err != nil {
		return dns.RcodeServerFailure, err
	}

	if len(records) == 0 {
		return plugin.BackendError(ctx, &k, zone, dns.RcodeSuccess, state, nil, plugin.Options{})
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = append(m.Answer, records...)
	m.Extra = append(m.Extra, extra...)

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

// get records from cache
func (k Kubernetes) getRecords(ctx context.Context, state request.Request) ([]dns.RR, []dns.RR, string, error) {
	qname := state.QName()
	zone := plugin.Zones(k.Zones).Matches(qname)

	zone = qname[len(qname)-len(zone):] // maintain case of original query
	state.Zone = zone

	var (
		records []dns.RR
		extra   []dns.RR
		err     error
	)

	switch state.QType() {
	case dns.TypeAXFR, dns.TypeIXFR:
		k.Transfer(ctx, state)
	case dns.TypeA:
		records, err = plugin.A(ctx, &k, zone, state, nil, plugin.Options{})
	case dns.TypeAAAA:
		records, err = plugin.AAAA(ctx, &k, zone, state, nil, plugin.Options{})
	case dns.TypeTXT:
		records, err = plugin.TXT(ctx, &k, zone, state, nil, plugin.Options{})
	case dns.TypeCNAME:
		records, err = plugin.CNAME(ctx, &k, zone, state, plugin.Options{})
	case dns.TypePTR:
		records, err = plugin.PTR(ctx, &k, zone, state, plugin.Options{})
	case dns.TypeMX:
		records, extra, err = plugin.MX(ctx, &k, zone, state, plugin.Options{})
	case dns.TypeSRV:
		records, extra, err = plugin.SRV(ctx, &k, zone, state, plugin.Options{})
	case dns.TypeSOA:
		records, err = plugin.SOA(ctx, &k, zone, state, plugin.Options{})
	case dns.TypeNS:
		if state.Name() == zone {
			records, extra, err = plugin.NS(ctx, &k, zone, state, plugin.Options{})
			break
		}
		fallthrough
	default:
		// Do a fake A lookup, so we can distinguish between NODATA and NXDOMAIN
		fake := state.NewWithQuestion(state.QName(), dns.TypeA)
		fake.Zone = state.Zone
		_, err = plugin.A(ctx, &k, zone, fake, nil, plugin.Options{})
	}

	return records, extra, zone, err
}

// Name implements the Handler interface.
func (k Kubernetes) Name() string { return "k8s_dns_chaos" }
