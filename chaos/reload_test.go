package chaos

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	_ "github.com/coredns/coredns/plugin/bind"
	_ "github.com/coredns/coredns/plugin/whoami"
	"github.com/miekg/dns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func reserveTCPPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener, listener.Addr().(*net.TCPAddr).Port
}

func unusedTCPPort(t *testing.T) int {
	t.Helper()
	listener, port := reserveTCPPort(t)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestCoreDNSReloadPreservesRules(t *testing.T) {
	testCoreDNSReload(t, true, ActionError)
}

func TestCoreDNSFailedReloadKeepsRPCOwner(t *testing.T) {
	testCoreDNSReload(t, false, ActionError)
}

func TestCoreDNSReloadPreservesStaticRules(t *testing.T) {
	testCoreDNSReload(t, true, ActionStatic)
}

func testCoreDNSReload(t *testing.T, successfulReload bool, action string) {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"target","namespace":"default"},"status":{"podIP":"127.0.0.1"}}`)
	}))
	defer api.Close()
	configPath := filepath.Join(t.TempDir(), "kubeconfig")
	config := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\ncurrent-context: test\n", api.URL)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	oldDirectives := dnsserver.Directives
	dnsserver.Directives = []string{"bind", "dns_chaos", "whoami"}
	t.Cleanup(func() { dnsserver.Directives = oldDirectives })
	dnsPort, grpcPort := unusedTCPPort(t), unusedTCPPort(t)
	corefile := func(port int) caddy.Input {
		return caddy.CaddyfileInput{ServerTypeName: "dns", Contents: []byte(fmt.Sprintf(".:%d {\n bind 127.0.0.1\n dns_chaos {\n grpcport %d\n kubeconfig %s test\n }\n whoami\n}\n", port, grpcPort, configPath))}
	}
	instance, err := caddy.Start(corefile(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Stop(); instance.ShutdownCallbacks() })
	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewDNSClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{Name: "reload", Action: action,
		IpDomainMaps: []*pb.IpDomainMap{{Domain: "example.org", Ip: "192.0.2.20"}},
		Pods:         []*pb.Pod{{Namespace: "default", Name: "target"}}}); err != nil {
		t.Fatal(err)
	}
	assertDNS := func(injected bool) {
		want := dns.RcodeSuccess
		if injected && action == ActionError {
			want = dns.RcodeServerFailure
		}
		t.Helper()
		query := new(dns.Msg)
		query.SetQuestion("example.org.", dns.TypeA)
		response, _, err := (&dns.Client{Timeout: time.Second}).Exchange(query, fmt.Sprintf("127.0.0.1:%d", dnsPort))
		if err != nil {
			t.Fatal(err)
		}
		if response.Rcode != want {
			t.Fatalf("DNS response code = %d, want %d", response.Rcode, want)
		}
		if action == ActionStatic {
			wantIP := "127.0.0.1"
			if injected {
				wantIP = "192.0.2.20"
			}
			records := response.Answer
			if !injected {
				if len(response.Answer) != 0 {
					t.Fatalf("recovered answers = %v, want empty whoami answer", response.Answer)
				}
				records = response.Extra
			}
			if len(records) == 0 {
				t.Fatalf("missing address in DNS response %v", response)
			}
			answer, ok := records[0].(*dns.A)
			if !ok || answer.A.String() != wantIP {
				t.Fatalf("DNS address records = %v, want %s", records, wantIP)
			}
		}
	}
	assertDNS(true)
	if successfulReload {
		instance, err = instance.Restart(corefile(dnsPort))
		if err != nil {
			t.Fatal(err)
		}
		assertDNS(true)
	}
	occupied, occupiedPort := reserveTCPPort(t)
	defer occupied.Close()
	if _, err := instance.Restart(corefile(occupiedPort)); err == nil {
		t.Fatal("reload onto occupied DNS port must fail")
	}
	assertDNS(true)
	if _, err := client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: "reload"}); err != nil {
		t.Fatal(err)
	}
	assertDNS(false)
	if !successfulReload {
		return
	}

	setRule := func() {
		t.Helper()
		if _, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{Name: "reload", Action: action,
			IpDomainMaps: []*pb.IpDomainMap{{Domain: "example.org", Ip: "192.0.2.20"}},
			Pods:         []*pb.Pod{{Namespace: "default", Name: "target"}}}); err != nil {
			t.Fatal(err)
		}
	}
	assertPortReleased := func(port int) {
		t.Helper()
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("port %d still occupied: %v", port, err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	setRule()
	oldPort := grpcPort
	grpcPort = unusedTCPPort(t)
	oldInstance := instance
	instance, err = instance.Restart(corefile(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertDNS(true)
	assertPortReleased(oldPort)
	for _, callback := range oldInstance.OnShutdown {
		if err := callback(); err != nil {
			t.Fatal(err)
		}
	}
	newConn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", grpcPort), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer newConn.Close()
	client = pb.NewDNSClient(newConn)
	setRule()

	activePort := grpcPort
	grpcPort = unusedTCPPort(t)
	if _, err := instance.Restart(corefile(occupiedPort)); err == nil {
		t.Fatal("changed-port reload onto occupied DNS port must fail")
	}
	assertPortReleased(grpcPort)
	grpcPort = activePort
	assertDNS(true)
	setRule()

	occupiedRPC, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	grpcPort = occupiedRPC.Addr().(*net.TCPAddr).Port
	if next, err := instance.Restart(corefile(dnsPort)); err == nil {
		instance = next
		t.Fatal("reload onto occupied RPC port must fail")
	}
	if err := occupiedRPC.Close(); err != nil {
		t.Fatal(err)
	}
	grpcPort = activePort
	setRule()

	originalPath := configPath
	configPath += "-different"
	if _, err := instance.Restart(corefile(dnsPort)); err == nil {
		t.Fatal("live Kubernetes config change must require restart")
	}
	configPath = originalPath
	instance, err = instance.Restart(corefile(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertDNS(true)
	setRule()

	withoutChaos := func(port int) caddy.Input {
		return caddy.CaddyfileInput{ServerTypeName: "dns", Contents: []byte(fmt.Sprintf(".:%d {\n bind 127.0.0.1\n whoami\n}\n", port))}
	}
	if _, err := instance.Restart(withoutChaos(occupiedPort)); err == nil {
		t.Fatal("failed directive removal must retain RPC")
	}
	setRule()
	assertDNS(true)
	instance, err = instance.Restart(withoutChaos(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertPortReleased(grpcPort)
	assertDNS(false)
	if next, err := instance.Restart(corefile(occupiedPort)); err == nil {
		instance = next
		t.Fatal("re-enabling onto occupied DNS port must fail")
	}
	assertPortReleased(grpcPort)
	assertDNS(false)
	instance, err = instance.Restart(corefile(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertDNS(false)
	setRule()
	assertDNS(true)
	if _, err := client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: "reload"}); err != nil {
		t.Fatal(err)
	}
	assertDNS(false)
}
