package chaos

import (
	"context"
	"testing"

	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

func TestRandomChaosFallsThroughForNonAddressQuery(t *testing.T) {
	next := &testHandler{}
	chaos := New()
	chaos.Next = next
	requestMessage := new(dns.Msg)
	requestMessage.SetQuestion("service.example.", dns.TypeSRV)

	rcode, err := chaos.applyChaos(
		context.Background(),
		nil,
		requestMessage,
		request.Request{Req: requestMessage},
		&PodInfo{Action: ActionRandom},
	)
	if err != nil {
		t.Fatalf("applyChaos() error = %v", err)
	}
	if rcode != dns.RcodeSuccess {
		t.Fatalf("applyChaos() rcode = %d, want %d", rcode, dns.RcodeSuccess)
	}
	if next.calls != 1 {
		t.Fatalf("downstream handler calls = %d, want 1", next.calls)
	}
}

type testHandler struct {
	calls int
}

func (h *testHandler) Name() string {
	return "test"
}

func (h *testHandler) ServeDNS(context.Context, dns.ResponseWriter, *dns.Msg) (int, error) {
	h.calls++
	return dns.RcodeSuccess, nil
}
