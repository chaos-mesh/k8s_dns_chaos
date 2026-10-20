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
	testCoreDNSReload(t, true)
}

func TestCoreDNSFailedReloadKeepsRPCOwner(t *testing.T) {
	testCoreDNSReload(t, false)
}

func testCoreDNSReload(t *testing.T, successfulReload bool) {
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
	if _, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{Name: "reload", Action: ActionError,
		Pods: []*pb.Pod{{Namespace: "default", Name: "target"}}}); err != nil {
		t.Fatal(err)
	}
	assertDNS := func(want int) {
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
	}
	assertDNS(dns.RcodeServerFailure)
	if successfulReload {
		instance, err = instance.Restart(corefile(dnsPort))
		if err != nil {
			t.Fatal(err)
		}
		assertDNS(dns.RcodeServerFailure)
	}
	occupied, occupiedPort := reserveTCPPort(t)
	defer occupied.Close()
	if _, err := instance.Restart(corefile(occupiedPort)); err == nil {
		t.Fatal("reload onto occupied DNS port must fail")
	}
	assertDNS(dns.RcodeServerFailure)
	if _, err := client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: "reload"}); err != nil {
		t.Fatal(err)
	}
	assertDNS(dns.RcodeSuccess)
	if !successfulReload {
		return
	}

	setRule := func() {
		t.Helper()
		if _, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{Name: "reload", Action: ActionError,
			Pods: []*pb.Pod{{Namespace: "default", Name: "target"}}}); err != nil {
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
	assertDNS(dns.RcodeServerFailure)
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
	assertDNS(dns.RcodeServerFailure)
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
	assertDNS(dns.RcodeServerFailure)
	setRule()

	withoutChaos := func(port int) caddy.Input {
		return caddy.CaddyfileInput{ServerTypeName: "dns", Contents: []byte(fmt.Sprintf(".:%d {\n bind 127.0.0.1\n whoami\n}\n", port))}
	}
	if _, err := instance.Restart(withoutChaos(occupiedPort)); err == nil {
		t.Fatal("failed directive removal must retain RPC")
	}
	setRule()
	assertDNS(dns.RcodeServerFailure)
	instance, err = instance.Restart(withoutChaos(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertPortReleased(grpcPort)
	assertDNS(dns.RcodeSuccess)
	if next, err := instance.Restart(corefile(occupiedPort)); err == nil {
		instance = next
		t.Fatal("re-enabling onto occupied DNS port must fail")
	}
	assertPortReleased(grpcPort)
	assertDNS(dns.RcodeSuccess)
	instance, err = instance.Restart(corefile(dnsPort))
	if err != nil {
		t.Fatal(err)
	}
	assertDNS(dns.RcodeSuccess)
	setRule()
	assertDNS(dns.RcodeServerFailure)
	if _, err := client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: "reload"}); err != nil {
		t.Fatal(err)
	}
	assertDNS(dns.RcodeSuccess)
}
