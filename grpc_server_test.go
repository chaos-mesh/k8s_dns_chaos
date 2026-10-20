package kubernetes

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLegacyDNSRPC(t *testing.T) {
	k := New([]string{"cluster.local."})
	k.APIConn = &APIConnServeTest{}
	k.Client = fake.NewSimpleClientset(&api.Pod{
		ObjectMeta: meta.ObjectMeta{Namespace: "testns", Name: "client"},
		Status:     api.PodStatus{PodIP: "10.240.0.1"},
	}).CoreV1()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterDNSServer(server, k)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-serveDone; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("gRPC server: %v", err)
		}
	})
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := new(dns.Msg)
	query.SetQuestion("svc1.testns.svc.cluster.local.", dns.TypeA)
	request := &pb.SetDNSChaosRequest{
		Name: "legacy-wire", Action: ActionError,
		Patterns: []string{"svc1.testns.svc.cluster.local"},
		Pods:     []*pb.Pod{{Namespace: "testns", Name: "client"}},
	}
	var setResponse pb.DNSChaosResponse
	if err := conn.Invoke(ctx, "/pb.DNS/SetDNSChaos", request, &setResponse); err != nil {
		t.Fatal(err)
	}
	if !setResponse.Result {
		t.Fatal("SetDNSChaos returned false")
	}
	w := dnstest.NewRecorder(&test.ResponseWriter{})
	if rcode, err := k.ServeDNS(ctx, w, query); rcode != dns.RcodeServerFailure || err == nil {
		t.Fatalf("injected DNS = %d, %v; want SERVFAIL and chaos error", rcode, err)
	}
	var cancelResponse pb.DNSChaosResponse
	if err := conn.Invoke(ctx, "/pb.DNS/CancelDNSChaos", &pb.CancelDNSChaosRequest{Name: request.Name}, &cancelResponse); err != nil {
		t.Fatal(err)
	}
	if !cancelResponse.Result {
		t.Fatal("CancelDNSChaos returned false")
	}
	w = dnstest.NewRecorder(&test.ResponseWriter{})
	if rcode, err := k.ServeDNS(ctx, w, query); rcode != dns.RcodeSuccess || err != nil {
		t.Fatalf("recovered DNS = %d, %v", rcode, err)
	}
	if w.Msg == nil {
		t.Fatal("no DNS response after cancelling chaos")
	}
	if err := test.SortAndCheck(w.Msg, test.Case{
		Rcode:  dns.RcodeSuccess,
		Answer: []dns.RR{test.A("svc1.testns.svc.cluster.local. 5 IN A 10.0.0.1")},
	}); err != nil {
		t.Fatal(err)
	}
}
