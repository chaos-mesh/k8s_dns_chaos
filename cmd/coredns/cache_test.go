package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaos-mesh/k8s_dns_chaos/chaos"
	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/coredns/caddy"
	"github.com/coredns/caddy/caddyfile"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDNSChaosCacheIsolation(t *testing.T) {
	for _, cacheFirst := range []bool{true, false} {
		for _, warm := range []bool{true, false} {
			for _, family := range []struct {
				name               string
				qtype, opposite    uint16
				first, second, new string
			}{
				{"IPv4", dns.TypeA, dns.TypeAAAA, "192.0.2.1", "192.0.2.2", "192.0.2.3"},
				{"IPv6", dns.TypeAAAA, dns.TypeA, "2001:db8::1", "2001:db8::2", "2001:db8::3"},
			} {
				t.Run(fmt.Sprintf("cacheFirst=%t/warm=%t/%s", cacheFirst, warm, family.name), func(t *testing.T) {
					server, fault, calls := cacheTestServer(t, cacheFirst)
					query := func(source string, qtype uint16, want string) {
						t.Helper()
						checkCacheAnswer(t, cacheQuery(server, source, qtype), dns.RcodeSuccess, want)
					}
					if warm {
						for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
							query("10.0.0.1", qtype, normalCacheIP(qtype))
							query("10.0.0.3", qtype, normalCacheIP(qtype))
						}
						if *calls != 2 {
							t.Fatalf("normal repeated queries made %d downstream calls, want 2", *calls)
						}
					}

					setCacheRule(t, fault, "first", chaos.ActionStatic, family.first)
					setCacheRule(t, fault, "second", chaos.ActionStatic, family.second)
					query("10.0.0.1", family.qtype, family.first)
					query("10.0.0.2", family.qtype, family.second)
					query("10.0.0.1", family.opposite, "")
					query("10.0.0.2", family.opposite, "")
					for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
						query("10.0.0.3", qtype, normalCacheIP(qtype))
						query("10.0.0.3", qtype, normalCacheIP(qtype))
					}

					setCacheRule(t, fault, "first", chaos.ActionStatic, family.new)
					query("10.0.0.1", family.qtype, family.new)
					query("10.0.0.2", family.qtype, family.second)
					for _, name := range []string{"first", "second"} {
						if _, err := fault.CancelDNSChaos(context.Background(), &pb.CancelDNSChaosRequest{Name: name}); err != nil {
							t.Fatal(err)
						}
					}
					for _, source := range []string{"10.0.0.1", "10.0.0.2"} {
						for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
							query(source, qtype, normalCacheIP(qtype))
						}
					}
					if *calls != 2 {
						t.Fatalf("normal answers were not retained in cache: downstream calls = %d, want 2", *calls)
					}
				})
			}
		}
	}
}

func TestDNSChaosCacheErrorAndRandom(t *testing.T) {
	for _, action := range []string{chaos.ActionError, chaos.ActionRandom} {
		for _, warm := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/warm=%t", action, warm), func(t *testing.T) {
				server, fault, calls := cacheTestServer(t, true)
				if warm {
					checkCacheAnswer(t, cacheQuery(server, "10.0.0.1", dns.TypeA), dns.RcodeSuccess, normalCacheIP(dns.TypeA))
				}
				setCacheRule(t, fault, "first", action, "")
				for range 2 {
					response := cacheQuery(server, "10.0.0.1", dns.TypeA)
					if action == chaos.ActionError {
						checkCacheAnswer(t, response, dns.RcodeServerFailure, "")
					} else if response == nil || response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 ||
						response.Answer[0].Header().Rrtype != dns.TypeA || response.Answer[0].Header().Ttl != 10 {
						t.Fatalf("expected random A response with TTL 10, got %v", response)
					}
				}
				checkCacheAnswer(t, cacheQuery(server, "10.0.0.3", dns.TypeA), dns.RcodeSuccess, normalCacheIP(dns.TypeA))
				if _, err := fault.CancelDNSChaos(context.Background(), &pb.CancelDNSChaosRequest{Name: "first"}); err != nil {
					t.Fatal(err)
				}
				checkCacheAnswer(t, cacheQuery(server, "10.0.0.1", dns.TypeA), dns.RcodeSuccess, normalCacheIP(dns.TypeA))
				if *calls != 1 {
					t.Fatalf("downstream calls = %d, want 1", *calls)
				}
			})
		}
	}
}

func cacheTestServer(t *testing.T, cacheFirst bool) (*dnsserver.Server, *chaos.DNSChaos, *int) {
	t.Helper()
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: http://127.0.0.1:1
contexts:
- name: test
  context:
    cluster: test
current-context: test
`), 0600); err != nil {
		t.Fatal(err)
	}
	directives := []string{fmt.Sprintf("dns_chaos {\n kubeconfig %s test\n}", kubeconfig), "cache 30"}
	if cacheFirst {
		directives[0], directives[1] = directives[1], directives[0]
	}
	blocks, err := caddyfile.Parse("Corefile", strings.NewReader(". {\n"+strings.Join(directives, "\n")+"\n}"), dnsserver.Directives)
	if err != nil {
		t.Fatal(err)
	}
	controller := caddy.NewTestController("dns", "")
	controller.ServerBlockKeys = blocks[0].Keys
	config := dnsserver.GetConfig(controller)
	config.Zone = "."
	config.Debug = true
	for _, name := range dnsserver.Directives {
		tokens, enabled := blocks[0].Tokens[name]
		if !enabled {
			continue
		}
		setup, err := caddy.DirectiveAction("dns", name)
		if err != nil {
			t.Fatal(err)
		}
		controller.Dispenser = caddyfile.NewDispenserTokens("Corefile", tokens)
		if err := setup(controller); err != nil {
			t.Fatal(err)
		}
	}
	calls := new(int)
	config.AddPlugin(func(plugin.Handler) plugin.Handler {
		return plugin.HandlerFunc(func(_ context.Context, w dns.ResponseWriter, request *dns.Msg) (int, error) {
			*calls++
			response := new(dns.Msg)
			response.SetReply(request)
			response.Authoritative = true
			qtype := request.Question[0].Qtype
			answer, err := dns.NewRR(fmt.Sprintf("%s 30 IN %s %s", request.Question[0].Name, dns.TypeToString[qtype], normalCacheIP(qtype)))
			if err != nil {
				return dns.RcodeServerFailure, err
			}
			response.Answer = []dns.RR{answer}
			return dns.RcodeSuccess, w.WriteMsg(response)
		})
	})
	server, err := dnsserver.NewServer("127.0.0.1:0", []*dnsserver.Config{config})
	if err != nil {
		t.Fatal(err)
	}
	fault := config.Handler("dns_chaos").(*chaos.DNSChaos)
	fault.Client = fake.NewSimpleClientset(
		&api.Pod{ObjectMeta: meta.ObjectMeta{Namespace: "default", Name: "first"}, Status: api.PodStatus{PodIP: "10.0.0.1"}},
		&api.Pod{ObjectMeta: meta.ObjectMeta{Namespace: "default", Name: "second"}, Status: api.PodStatus{PodIP: "10.0.0.2"}},
	).CoreV1()
	return server, fault, calls
}

func setCacheRule(t *testing.T, fault *chaos.DNSChaos, name, action, ip string) {
	t.Helper()
	request := &pb.SetDNSChaosRequest{
		Name: name, Action: action, Pods: []*pb.Pod{{Namespace: "default", Name: name}},
	}
	if action == chaos.ActionStatic {
		request.IpDomainMaps = []*pb.IpDomainMap{{Domain: "example.test", Ip: ip}}
	}
	if _, err := fault.SetDNSChaos(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func cacheQuery(server *dnsserver.Server, source string, qtype uint16) *dns.Msg {
	request := new(dns.Msg)
	request.SetQuestion("example.test.", qtype)
	recorder := dnstest.NewRecorder(&test.ResponseWriter{RemoteIP: source})
	server.ServeDNS(context.Background(), recorder, request)
	return recorder.Msg
}

func checkCacheAnswer(t *testing.T, response *dns.Msg, rcode int, ip string) {
	t.Helper()
	if response == nil || response.Rcode != rcode {
		t.Fatalf("expected rcode %s, got %v", dns.RcodeToString[rcode], response)
	}
	if ip == "" {
		if len(response.Answer) != 0 {
			t.Fatalf("expected empty answer, got %v", response.Answer)
		}
		return
	}
	if len(response.Answer) == 1 {
		var got net.IP
		switch answer := response.Answer[0].(type) {
		case *dns.A:
			got = answer.A
		case *dns.AAAA:
			got = answer.AAAA
		}
		if got.Equal(net.ParseIP(ip)) {
			return
		}
	}
	t.Fatalf("expected IP %s, got %v", ip, response.Answer)
}

func normalCacheIP(qtype uint16) string {
	if qtype == dns.TypeAAAA {
		return "2001:db8::100"
	}
	return "192.0.2.100"
}
