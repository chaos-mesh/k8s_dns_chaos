package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/pkg/upstream"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

func TestServeDNSExternalCNAMETruncated(t *testing.T) {
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		for _, truncated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/truncated=%t", dns.TypeToString[qtype], truncated), func(t *testing.T) {
				lookupCalled := false
				externalAnswer := dns.RR(test.A("ext.interwebs.test. 30 IN A 192.0.2.1"))
				if qtype == dns.TypeAAAA {
					externalAnswer = test.AAAA("ext.interwebs.test. 30 IN AAAA 2001:db8::1")
				}
				config := &dnsserver.Config{Zone: "."}
				config.AddPlugin(func(plugin.Handler) plugin.Handler {
					return plugin.HandlerFunc(func(_ context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
						lookupCalled = true
						if r.Question[0].Name != "ext.interwebs.test." || r.Question[0].Qtype != qtype {
							t.Errorf("unexpected external lookup: %v", r.Question)
						}
						reply := new(dns.Msg)
						reply.SetReply(r)
						reply.Truncated = truncated
						reply.Answer = []dns.RR{externalAnswer}
						return dns.RcodeSuccess, w.WriteMsg(reply)
					})
				})
				server, err := dnsserver.NewServer("dns://127.0.0.1:0", []*dnsserver.Config{config})
				if err != nil {
					t.Fatal(err)
				}
				k := New([]string{"cluster.local."})
				k.APIConn = &APIConnServeTest{}
				k.Upstream = upstream.New()
				ctx := context.WithValue(context.Background(), dnsserver.Key{}, server)
				query := new(dns.Msg)
				query.SetQuestion("external.testns.svc.cluster.local.", qtype)
				w := dnstest.NewRecorder(&test.ResponseWriter{})
				rcode, err := k.ServeDNS(ctx, w, query)
				if err != nil || rcode != dns.RcodeSuccess {
					t.Fatalf("ServeDNS = %d, %v", rcode, err)
				}
				if !lookupCalled || w.Msg == nil {
					t.Fatal("external lookup did not produce a DNS response")
				}
				if w.Msg.Truncated != truncated {
					t.Errorf("response TC = %t, want %t", w.Msg.Truncated, truncated)
				}
				want := test.Case{
					Rcode: dns.RcodeSuccess,
					Answer: []dns.RR{
						test.CNAME("external.testns.svc.cluster.local. 5 IN CNAME ext.interwebs.test."),
						externalAnswer,
					},
				}
				if err := test.CNAMEOrder(w.Msg); err != nil {
					t.Fatal(err)
				}
				sort.Sort(test.RRSet(want.Answer))
				if err := test.SortAndCheck(w.Msg, want); err != nil {
					t.Error(err)
				}
			})
		}
	}
}
