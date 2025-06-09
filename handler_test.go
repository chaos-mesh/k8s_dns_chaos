package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/coredns/coredns/plugin/kubernetes/object"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var dnsTestCases = []test.Case{
	// A Service
	{
		Qname: "svc1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc1.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	{
		Qname: "svcempty.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svcempty.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	// A Service (wildcard)
	{
		Qname: "svc1.*.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc1.*.svc.cluster.local.  5       IN      A       10.0.0.1"),
		},
	},
	{
		Qname: "svc1.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("svc1.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svc1.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.A("svc1.testns.svc.cluster.local.  5       IN      A       10.0.0.1")},
	},
	{
		Qname: "svcempty.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("svcempty.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svcempty.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.A("svcempty.testns.svc.cluster.local.  5       IN      A       10.0.0.1")},
	},
	{
		Qname: "svc6.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("svc6.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svc6.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.AAAA("svc6.testns.svc.cluster.local.  5       IN      AAAA       1234:abcd::1")},
	},
	// SRV Service (wildcard)
	{
		Qname: "svc1.*.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("svc1.*.svc.cluster.local.	5	IN	SRV	0 100 80 svc1.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.A("svc1.testns.svc.cluster.local.  5       IN      A       10.0.0.1")},
	},
	{
		Qname: "svcempty.*.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("svcempty.*.svc.cluster.local.	5	IN	SRV	0 100 80 svcempty.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.A("svcempty.testns.svc.cluster.local.  5       IN      A       10.0.0.1")},
	},
	// SRV Service (wildcards)
	{
		Qname: "*.any.svc1.*.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.SRV("*.any.svc1.*.svc.cluster.local.	5	IN	SRV	0 100 80 svc1.testns.svc.cluster.local.")},
		Extra:  []dns.RR{test.A("svc1.testns.svc.cluster.local.  5       IN      A       10.0.0.1")},
	},
	// A Service (wildcards)
	{
		Qname: "*.any.svc1.*.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("*.any.svc1.*.svc.cluster.local.  5       IN      A       10.0.0.1"),
		},
	},
	// SRV Service Not udp/tcp
	{
		Qname: "*._not-udp-or-tcp.svc1.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// SRV Service
	{
		Qname: "_http._tcp.svc1.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.SRV("_http._tcp.svc1.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svc1.testns.svc.cluster.local."),
		},
		Extra: []dns.RR{
			test.A("svc1.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	{
		Qname: "_http._tcp.svcempty.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.SRV("_http._tcp.svcempty.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svcempty.testns.svc.cluster.local."),
		},
		Extra: []dns.RR{
			test.A("svcempty.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	// A Service (Headless)
	{
		Qname: "hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.2"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.3"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.4"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.5"),
		},
	},
	// A Service (Headless and Portless)
	{
		Qname: "hdlsprtls.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("hdlsprtls.testns.svc.cluster.local.	5	IN	A	172.0.0.20"),
		},
	},
	// An Endpoint with no port
	{
		Qname: "172-0-0-20.hdlsprtls.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("172-0-0-20.hdlsprtls.testns.svc.cluster.local.	5	IN	A	172.0.0.20"),
		},
	},
	// An Endpoint ip
	{
		Qname: "172-0-0-2.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("172-0-0-2.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.2"),
		},
	},
	// A Endpoint ip
	{
		Qname: "172-0-0-3.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("172-0-0-3.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.3"),
		},
	},
	// An Endpoint by name
	{
		Qname: "dup-name.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("dup-name.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.4"),
			test.A("dup-name.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.5"),
		},
	},
	// SRV Service (Headless)
	{
		Qname: "_http._tcp.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.SRV("_http._tcp.hdls1.testns.svc.cluster.local.	5	IN	SRV	0 16 80 172-0-0-2.hdls1.testns.svc.cluster.local."),
			test.SRV("_http._tcp.hdls1.testns.svc.cluster.local.	5	IN	SRV	0 16 80 172-0-0-3.hdls1.testns.svc.cluster.local."),
			test.SRV("_http._tcp.hdls1.testns.svc.cluster.local.	5	IN	SRV	0 16 80 5678-abcd--1.hdls1.testns.svc.cluster.local."),
			test.SRV("_http._tcp.hdls1.testns.svc.cluster.local.	5	IN	SRV	0 16 80 5678-abcd--2.hdls1.testns.svc.cluster.local."),
			test.SRV("_http._tcp.hdls1.testns.svc.cluster.local.	5	IN	SRV	0 16 80 dup-name.hdls1.testns.svc.cluster.local."),
		},
		Extra: []dns.RR{
			test.A("172-0-0-2.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.2"),
			test.A("172-0-0-3.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.3"),
			test.AAAA("5678-abcd--1.hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::1"),
			test.AAAA("5678-abcd--2.hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::2"),
			test.A("dup-name.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.4"),
			test.A("dup-name.hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.5"),
		},
	},
	{ // An A record query for an existing headless service should return a record for each of its ipv4 endpoints
		Qname: "hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.2"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.3"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.4"),
			test.A("hdls1.testns.svc.cluster.local.	5	IN	A	172.0.0.5"),
		},
	},
	// SRV Service (Headless and portless)
	{
		Qname: "*.*.hdlsprtls.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// AAAA
	{
		Qname: "5678-abcd--2.hdls1.testns.svc.cluster.local", Qtype: dns.TypeAAAA,
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.AAAA("5678-abcd--2.hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::2")},
	},
	// CNAME External
	{
		Qname: "external.testns.svc.cluster.local.", Qtype: dns.TypeCNAME,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("external.testns.svc.cluster.local.	5	IN	CNAME	ext.interwebs.test."),
		},
	},
	// CNAME External To Internal Service
	{
		Qname: "external-to-service.testns.svc.cluster.local", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.CNAME("external-to-service.testns.svc.cluster.local.	5	IN	CNAME	svc1.testns.svc.cluster.local."),
			test.A("svc1.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	// AAAA Service (with an existing A record, but no AAAA record)
	{
		Qname: "svc1.testns.svc.cluster.local.", Qtype: dns.TypeAAAA,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// AAAA Service (non-existing service)
	{
		Qname: "svc0.testns.svc.cluster.local.", Qtype: dns.TypeAAAA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// A Service (non-existing service)
	{
		Qname: "svc0.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// A Service (non-existing namespace)
	{
		Qname: "svc0.svc-nons.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// TXT Schema
	{
		Qname: "dns-version.cluster.local.", Qtype: dns.TypeTXT,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.TXT("dns-version.cluster.local 28800 IN TXT 1.1.0"),
		},
	},
	// A Service (Headless) does not exist
	{
		Qname: "bogusendpoint.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// A Service does not exist
	{
		Qname: "bogusendpoint.svc0.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// AAAA Service
	{
		Qname: "svc6.testns.svc.cluster.local.", Qtype: dns.TypeAAAA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.AAAA("svc6.testns.svc.cluster.local.	5	IN	AAAA	1234:abcd::1"),
		},
	},
	// SRV
	{
		Qname: "_http._tcp.svc6.testns.svc.cluster.local.", Qtype: dns.TypeSRV,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.SRV("_http._tcp.svc6.testns.svc.cluster.local.	5	IN	SRV	0 100 80 svc6.testns.svc.cluster.local."),
		},
		Extra: []dns.RR{
			test.AAAA("svc6.testns.svc.cluster.local.	5	IN	AAAA	1234:abcd::1"),
		},
	},
	// AAAA Service (Headless)
	{
		Qname: "hdls1.testns.svc.cluster.local.", Qtype: dns.TypeAAAA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.AAAA("hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::1"),
			test.AAAA("hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::2"),
		},
	},
	// AAAA Endpoint
	{
		Qname: "5678-abcd--1.hdls1.testns.svc.cluster.local.", Qtype: dns.TypeAAAA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.AAAA("5678-abcd--1.hdls1.testns.svc.cluster.local.	5	IN	AAAA	5678:abcd::1"),
		},
	},

	{
		Qname: "svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	{
		Qname: "pod.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	{
		Qname: "testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// NS query for qname != zone (existing domain)
	{
		Qname: "svc.cluster.local.", Qtype: dns.TypeNS,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// NS query for qname != zone (existing domain)
	{
		Qname: "testns.svc.cluster.local.", Qtype: dns.TypeNS,
		Rcode: dns.RcodeSuccess,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// NS query for qname != zone (non existing domain)
	{
		Qname: "foo.cluster.local.", Qtype: dns.TypeNS,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
	// NS query for qname != zone (non existing domain)
	{
		Qname: "foo.svc.cluster.local.", Qtype: dns.TypeNS,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
}

func TestServeDNS(t *testing.T) {

	k := New([]string{"cluster.local."})
	k.APIConn = &APIConnServeTest{}
	k.Next = test.NextHandler(dns.RcodeSuccess, nil)
	k.Namespaces = map[string]struct{}{"testns": {}}
	ctx := context.TODO()

	for i, tc := range dnsTestCases {
		r := tc.Msg()

		w := dnstest.NewRecorder(&test.ResponseWriter{})

		_, err := k.ServeDNS(ctx, w, r)
		if err != tc.Error {
			t.Errorf("Test %d expected no error, got %v", i, err)
			return
		}
		if tc.Error != nil {
			continue
		}

		resp := w.Msg
		if resp == nil {
			t.Fatalf("Test %d, got nil message and no error for %q", i, r.Question[0].Name)
		}

		// Before sorting, make sure that CNAMES do not appear after their target records
		if err := test.CNAMEOrder(resp); err != nil {
			t.Error(err)
		}

		if err := test.SortAndCheck(resp, tc); err != nil {
			t.Error(err)
		}
	}
}

var nsTestCases = []test.Case{
	// A Service for an "exposed" namespace that "does exist"
	{
		Qname: "svc1.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeSuccess,
		Answer: []dns.RR{
			test.A("svc1.testns.svc.cluster.local.	5	IN	A	10.0.0.1"),
		},
	},
	// A service for an "exposed" namespace that "doesn't exist"
	{
		Qname: "svc1.nsnoexist.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeNameError,
		Ns: []dns.RR{
			test.SOA("cluster.local.	300	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1551484803 7200 1800 86400 30"),
		},
	},
}

func TestServeNamespaceDNS(t *testing.T) {
	k := New([]string{"cluster.local."})
	k.APIConn = &APIConnServeTest{}
	k.Next = test.NextHandler(dns.RcodeSuccess, nil)
	// if no namespaces are explicitly exposed, then they are all implicitly exposed
	k.Namespaces = map[string]struct{}{}
	ctx := context.TODO()

	for i, tc := range nsTestCases {
		r := tc.Msg()

		w := dnstest.NewRecorder(&test.ResponseWriter{})

		_, err := k.ServeDNS(ctx, w, r)
		if err != tc.Error {
			t.Errorf("Test %d expected no error, got %v", i, err)
			return
		}
		if tc.Error != nil {
			continue
		}

		resp := w.Msg
		if resp == nil {
			t.Fatalf("Test %d, got nil message and no error for %q", i, r.Question[0].Name)
		}

		// Before sorting, make sure that CNAMES do not appear after their target records
		test.CNAMEOrder(resp)

		test.SortAndCheck(resp, tc)
	}
}

var notSyncedTestCases = []test.Case{
	{
		// We should get ServerFailure instead of NameError for missing records when we kubernetes hasn't synced
		Qname: "svc0.testns.svc.cluster.local.", Qtype: dns.TypeA,
		Rcode: dns.RcodeServerFailure,
		Ns: []dns.RR{
			test.SOA("cluster.local.	5	IN	SOA	ns.dns.cluster.local. hostmaster.cluster.local. 1499347823 7200 1800 86400 5"),
		},
	},
}

func TestNotSyncedServeDNS(t *testing.T) {

	k := New([]string{"cluster.local."})
	k.APIConn = &APIConnServeTest{
		notSynced: true,
	}
	k.Next = test.NextHandler(dns.RcodeSuccess, nil)
	k.Namespaces = map[string]struct{}{"testns": {}}
	ctx := context.TODO()

	for i, tc := range notSyncedTestCases {
		r := tc.Msg()

		w := dnstest.NewRecorder(&test.ResponseWriter{})

		_, err := k.ServeDNS(ctx, w, r)
		if err != tc.Error {
			t.Errorf("Test %d expected no error, got %v", i, err)
			return
		}
		if tc.Error != nil {
			continue
		}

		resp := w.Msg
		if resp == nil {
			t.Fatalf("Test %d, got nil message and no error for %q", i, r.Question[0].Name)
		}

		if err := test.CNAMEOrder(resp); err != nil {
			t.Error(err)
		}

		if err := test.SortAndCheck(resp, tc); err != nil {
			t.Error(err)
		}
	}
}

type APIConnServeTest struct {
	notSynced bool
}

func (a APIConnServeTest) HasSynced() bool                         { return !a.notSynced }
func (APIConnServeTest) Run()                                      {}
func (APIConnServeTest) Stop() error                               { return nil }
func (APIConnServeTest) EpIndexReverse(string) []*object.Endpoints { return nil }
func (APIConnServeTest) SvcIndexReverse(string) []*object.Service  { return nil }
func (APIConnServeTest) Modified() int64                           { return time.Now().Unix() }

func (APIConnServeTest) PodIndex(ip string) []*object.Pod {
	if ip != "10.240.0.1" {
		return []*object.Pod{}
	}
	a := []*object.Pod{
		{Namespace: "podns", Name: "foo", PodIP: "10.240.0.1"}, // Remote IP set in test.ResponseWriter
	}
	return a
}

var svcIndex = map[string][]*object.Service{
	"svc1.testns": {
		{
			Name:      "svc1",
			Namespace: "testns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
	"svcempty.testns": {
		{
			Name:      "svcempty",
			Namespace: "testns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
	"svc6.testns": {
		{
			Name:      "svc6",
			Namespace: "testns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: "1234:abcd::1",
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
	"hdls1.testns": {
		{
			Name:      "hdls1",
			Namespace: "testns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: api.ClusterIPNone,
		},
	},
	"external.testns": {
		{
			Name:         "external",
			Namespace:    "testns",
			ExternalName: "ext.interwebs.test",
			Type:         api.ServiceTypeExternalName,
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
	"external-to-service.testns": {
		{
			Name:         "external-to-service",
			Namespace:    "testns",
			ExternalName: "svc1.testns.svc.cluster.local.",
			Type:         api.ServiceTypeExternalName,
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
	"hdlsprtls.testns": {
		{
			Name:      "hdlsprtls",
			Namespace: "testns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: api.ClusterIPNone,
		},
	},
	"svc1.unexposedns": {
		{
			Name:      "svc1",
			Namespace: "unexposedns",
			Type:      api.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.2",
			Ports: []api.ServicePort{
				{Name: "http", Protocol: "tcp", Port: 80},
			},
		},
	},
}

func (APIConnServeTest) SvcIndex(s string) []*object.Service { return svcIndex[s] }

func (APIConnServeTest) ServiceList() []*object.Service {
	var svcs []*object.Service
	for _, svc := range svcIndex {
		svcs = append(svcs, svc...)
	}
	return svcs
}

var epsIndex = map[string][]*object.Endpoints{
	"svc1.testns": {{
		Subsets: []object.EndpointSubset{
			{
				Addresses: []object.EndpointAddress{
					{IP: "172.0.0.1", Hostname: "ep1a"},
				},
				Ports: []object.EndpointPort{
					{Port: 80, Protocol: "tcp", Name: "http"},
				},
			},
		},
		Name:      "svc1",
		Namespace: "testns",
	}},
	"svcempty.testns": {{
		Subsets: []object.EndpointSubset{
			{
				Addresses: nil,
				Ports: []object.EndpointPort{
					{Port: 80, Protocol: "tcp", Name: "http"},
				},
			},
		},
		Name:      "svcempty",
		Namespace: "testns",
	}},
	"hdls1.testns": {{
		Subsets: []object.EndpointSubset{
			{
				Addresses: []object.EndpointAddress{
					{IP: "172.0.0.2"},
					{IP: "172.0.0.3"},
					{IP: "172.0.0.4", Hostname: "dup-name"},
					{IP: "172.0.0.5", Hostname: "dup-name"},
					{IP: "5678:abcd::1"},
					{IP: "5678:abcd::2"},
				},
				Ports: []object.EndpointPort{
					{Port: 80, Protocol: "tcp", Name: "http"},
				},
			},
		},
		Name:      "hdls1",
		Namespace: "testns",
	}},
	"hdlsprtls.testns": {{
		Subsets: []object.EndpointSubset{
			{
				Addresses: []object.EndpointAddress{
					{IP: "172.0.0.20"},
				},
				Ports: []object.EndpointPort{{Port: -1}},
			},
		},
		Name:      "hdlsprtls",
		Namespace: "testns",
	}},
}

func (APIConnServeTest) EpIndex(s string) []*object.Endpoints {
	return epsIndex[s]
}

func (APIConnServeTest) EndpointsList() []*object.Endpoints {
	var eps []*object.Endpoints
	for _, ep := range epsIndex {
		eps = append(eps, ep...)
	}
	return eps
}

func (APIConnServeTest) GetNodeByName(ctx context.Context, name string) (*api.Node, error) {
	return &api.Node{
		ObjectMeta: meta.ObjectMeta{
			Name: "test.node.foo.bar",
		},
	}, nil
}

func (APIConnServeTest) GetNamespaceByName(name string) (*api.Namespace, error) {
	if name == "pod-nons" { // handler_pod_verified_test.go uses this for non-existent namespace.
		return &api.Namespace{}, nil
	}
	if name == "nsnoexist" {
		return nil, fmt.Errorf("namespace not found")
	}
	return &api.Namespace{
		ObjectMeta: meta.ObjectMeta{
			Name: name,
		},
	}, nil
}
