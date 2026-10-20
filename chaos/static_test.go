package chaos

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStaticDNSOverGRPCAndUDP(t *testing.T) {
	c := New()
	c.Client = fake.NewSimpleClientset(testPod("target", "127.0.0.1"), testPod("second", "127.0.0.2")).CoreV1()
	c.Next = staticDownstream{}
	listener, port := reserveTCPPort(t)
	server := grpc.NewServer()
	pb.RegisterDNSServer(server, c.chaosRuntime)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := pb.NewDNSClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	set := func(pod, ip string) {
		t.Helper()
		_, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
			Name: "static", Action: "static", Pods: []*pb.Pod{{Namespace: "default", Name: pod}},
			IpDomainMaps: []*pb.IpDomainMap{{Domain: "Example.TEST.", Ip: ip}, {Domain: "ipv6.test", Ip: "2001:db8::1"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	set("target", "192.0.2.1")
	set("second", "192.0.2.2")
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dnsServer := &dns.Server{PacketConn: packet, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		if _, err := c.ServeDNS(context.Background(), w, r); err != nil {
			t.Errorf("ServeDNS: %v", err)
		}
	})}
	go func() { _ = dnsServer.ActivateAndServe() }()
	t.Cleanup(func() { _ = dnsServer.Shutdown(); _ = packet.Close() })
	query := func(source, domain string, qtype uint16, want string) {
		t.Helper()
		q := new(dns.Msg)
		q.SetQuestion(domain, qtype)
		var response *dns.Msg
		var err error
		if source == "127.0.0.1" {
			response, _, err = (&dns.Client{Timeout: time.Second}).Exchange(q, packet.LocalAddr().String())
		} else {
			recorder := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: source})
			_, err = c.ServeDNS(context.Background(), recorder, q)
			response = recorder.Msg
		}
		if err != nil {
			t.Fatal(err)
		}
		if response.Rcode != dns.RcodeSuccess {
			t.Fatalf("%s rcode = %d", domain, response.Rcode)
		}
		if want == "" {
			if len(response.Answer) != 0 {
				t.Fatalf("%s answers = %v, want empty", domain, response.Answer)
			}
			return
		}
		if len(response.Answer) != 1 {
			t.Fatalf("%s answers = %v", domain, response.Answer)
		}
		var got string
		switch rr := response.Answer[0].(type) {
		case *dns.A:
			got = rr.A.String()
		case *dns.AAAA:
			got = rr.AAAA.String()
		case *dns.TXT:
			got = rr.Txt[0]
		}
		if response.Answer[0].Header().Name != domain {
			t.Fatalf("answer owner = %q, want %q", response.Answer[0].Header().Name, domain)
		}
		if got != "203.0.113.1" && got != "downstream" && response.Answer[0].Header().Ttl != 10 {
			t.Fatalf("static TTL = %d, want 10", response.Answer[0].Header().Ttl)
		}
		if got != want {
			t.Fatalf("%s answer = %q, want %q", domain, got, want)
		}
	}
	query("127.0.0.1", "EXAMPLE.test.", dns.TypeA, "192.0.2.1")
	query("127.0.0.2", "example.test.", dns.TypeA, "192.0.2.2")
	query("127.0.0.3", "example.test.", dns.TypeA, "203.0.113.1")
	query("127.0.0.1", "example.test.", dns.TypeAAAA, "")
	query("127.0.0.1", "ipv6.test.", dns.TypeAAAA, "2001:db8::1")
	query("127.0.0.1", "ipv6.test.", dns.TypeA, "")
	query("127.0.0.1", "unmapped.test.", dns.TypeA, "203.0.113.1")
	query("127.0.0.1", "example.test.", dns.TypeTXT, "downstream")
	_, err = client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{Name: "static", Action: "static",
		Pods: []*pb.Pod{{Namespace: "default", Name: "target"}}, IpDomainMaps: []*pb.IpDomainMap{{Domain: "example.test", Ip: "invalid"}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid update error = %v", err)
	}
	query("127.0.0.1", "example.test.", dns.TypeA, "192.0.2.1")
	set("target", "192.0.2.10")
	query("127.0.0.1", "example.test.", dns.TypeA, "192.0.2.10")
	query("127.0.0.2", "example.test.", dns.TypeA, "192.0.2.2")
	if _, err := client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: "static"}); err != nil {
		t.Fatal(err)
	}
	query("127.0.0.1", "example.test.", dns.TypeA, "203.0.113.1")
	query("127.0.0.2", "example.test.", dns.TypeA, "203.0.113.1")
}

type staticDownstream struct{}

func (staticDownstream) Name() string { return "static-test-downstream" }

func (staticDownstream) ServeDNS(_ context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	m := new(dns.Msg)
	m.SetReply(r)
	if r.Question[0].Qtype == dns.TypeTXT {
		m.Answer = []dns.RR{&dns.TXT{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET}, Txt: []string{"downstream"}}}
	} else {
		m.Answer = a(r.Question[0].Name, 10, []net.IP{net.ParseIP("203.0.113.1")})
	}
	return dns.RcodeSuccess, w.WriteMsg(m)
}

func TestStaticRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		maps   []*pb.IpDomainMap
	}{
		{"empty", "static", nil},
		{"long label", "static", []*pb.IpDomainMap{{Domain: strings.Repeat("a", 64) + ".test", Ip: "192.0.2.1"}}},
		{"zoned IPv6", "static", []*pb.IpDomainMap{{Domain: "example.test", Ip: "fe80::1%eth0"}}},
		{"extra trailing dot", "static", []*pb.IpDomainMap{{Domain: "example.test..", Ip: "192.0.2.1"}}},
		{"nil", "static", []*pb.IpDomainMap{nil}},
		{"invalid IP", "static", []*pb.IpDomainMap{{Domain: "example.test", Ip: "bad"}}},
		{"empty domain", "static", []*pb.IpDomainMap{{Ip: "192.0.2.1"}}},
		{"invalid domain", "static", []*pb.IpDomainMap{{Domain: "example..test", Ip: "192.0.2.1"}}},
		{"wildcard", "static", []*pb.IpDomainMap{{Domain: "*.test", Ip: "192.0.2.1"}}},
		{"duplicate", "static", []*pb.IpDomainMap{{Domain: "EXAMPLE.test", Ip: "192.0.2.1"}, {Domain: "example.test.", Ip: "192.0.2.2"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New()
			_, err := c.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{Name: "invalid", Action: tc.action,
				Pods: []*pb.Pod{{Namespace: "default", Name: "target"}}, IpDomainMaps: tc.maps})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
		})
	}
}
