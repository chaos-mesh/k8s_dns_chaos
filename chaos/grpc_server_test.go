package chaos

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGRPCServerCanRestartOnSamePort(t *testing.T) {
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := portListener.Addr().(*net.TCPAddr).Port
	if err := portListener.Close(); err != nil {
		t.Fatalf("release reserved port: %v", err)
	}

	chaos := New()
	chaos.grpcPort = port
	t.Cleanup(func() {
		if err := chaos.StopGRPCServer(); err != nil {
			t.Errorf("StopGRPCServer() cleanup error = %v", err)
		}
	})

	if err := chaos.CreateGRPCServer(); err != nil {
		t.Fatalf("CreateGRPCServer() error = %v", err)
	}
	if err := chaos.StopGRPCServer(); err != nil {
		t.Fatalf("StopGRPCServer() error = %v", err)
	}
	if err := chaos.CreateGRPCServer(); err != nil {
		t.Fatalf("CreateGRPCServer() after stop error = %v", err)
	}
}

func TestSetDNSChaosIsAtomic(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("first", "10.0.0.1"))
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		getAction := action.(k8stesting.GetAction)
		if getAction.GetName() == "missing" {
			return true, nil, errors.New("kubernetes API is unavailable")
		}
		return false, nil, nil
	})

	chaos := New()
	chaos.Client = client.CoreV1()
	_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
		Name:   "atomic-rule",
		Action: ActionError,
		Pods: []*pb.Pod{
			{Namespace: "default", Name: "first"},
			{Namespace: "default", Name: "missing"},
		},
	})
	if err == nil {
		t.Fatal("expected SetDNSChaos to fail")
	}
	if len(chaos.chaosMap) != 0 || len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
		t.Fatal("failed rule must not modify chaos state")
	}
}

func TestSetDNSChaosRejectsOverlappingPod(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("target", "10.0.0.1"))
	chaos := New()
	chaos.Client = client.CoreV1()

	for _, request := range []*pb.SetDNSChaosRequest{
		{
			Name:   "first-rule",
			Action: ActionError,
			Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
		},
		{
			Name:   "second-rule",
			Action: ActionRandom,
			Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
		},
	} {
		_, err := chaos.SetDNSChaos(context.Background(), request)
		if request.Name == "first-rule" && err != nil {
			t.Fatalf("first rule error = %v", err)
		}
		if request.Name == "second-rule" && err == nil {
			t.Fatal("expected overlapping rule to be rejected")
		}
	}
}

func TestSetDNSChaosMergesExistingRulePods(t *testing.T) {
	client := fake.NewSimpleClientset(
		testPod("first", "10.0.0.1"),
		testPod("second", "10.0.0.2"),
	)
	chaos := New()
	chaos.Client = client.CoreV1()

	for _, podName := range []string{"first", "second"} {
		_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
			Name:   "replaced-rule",
			Action: ActionError,
			Pods:   []*pb.Pod{{Namespace: "default", Name: podName}},
		})
		if err != nil {
			t.Fatalf("SetDNSChaos(%s) error = %v", podName, err)
		}
	}

	if pod := chaos.getChaosPod(context.Background(), "10.0.0.1"); pod == nil || pod.Action != ActionError {
		t.Fatal("repeated rule must still target the first pod")
	}

	if _, ok := chaos.podMap["default"]["second"]; !ok {
		t.Fatal("repeated rule must target the second pod")
	}
	if _, err := chaos.CancelDNSChaos(context.Background(), &pb.CancelDNSChaosRequest{Name: "replaced-rule"}); err != nil {
		t.Fatal(err)
	}
	if len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
		t.Fatal("cancel must remove all incrementally added pods")
	}
}

func TestCancelDNSChaosDoesNotRemoveReassignedPod(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("target", "10.0.0.1"))
	chaos := New()
	chaos.Client = client.CoreV1()

	setRule := func(name string) {
		t.Helper()
		_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
			Name:   name,
			Action: ActionError,
			Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
		})
		if err != nil {
			t.Fatalf("SetDNSChaos(%s) error = %v", name, err)
		}
	}

	setRule("first-rule")
	chaos.Lock()
	chaos.removeRulePodsLocked("first-rule")
	chaos.Unlock()
	setRule("second-rule")

	_, err := chaos.CancelDNSChaos(context.Background(), &pb.CancelDNSChaosRequest{Name: "first-rule"})
	if err != nil {
		t.Fatalf("CancelDNSChaos() error = %v", err)
	}
	podInfo := chaos.podMap["default"]["target"]
	if podInfo == nil || podInfo.RuleName != "second-rule" {
		t.Fatal("cancelling the first rule must preserve the reassigned pod")
	}
}

func TestSetDNSChaosRejectsUnknownAction(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("target", "10.0.0.1"))
	chaos := New()
	chaos.Client = client.CoreV1()

	_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
		Name:   "invalid-action",
		Action: "invalid",
		Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
	})
	if err == nil {
		t.Fatal("expected an invalid action error")
	}
}

func TestSetDNSChaosRejectsPodWithoutIP(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("target", ""))
	chaos := New()
	chaos.Client = client.CoreV1()

	_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
		Name:   "pending-pod",
		Action: ActionError,
		Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
	})
	if err == nil {
		t.Fatal("expected a pod without an IP address to be rejected")
	}
	if len(chaos.chaosMap) != 0 || len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
		t.Fatal("pending pod must not modify chaos state")
	}
}

func TestSetDNSChaosRejectsDuplicatePodIP(t *testing.T) {
	client := fake.NewSimpleClientset(
		testPod("first", "10.0.0.1"),
		testPod("second", "10.0.0.1"),
	)
	chaos := New()
	chaos.Client = client.CoreV1()

	_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
		Name:   "duplicate-ip",
		Action: ActionError,
		Pods: []*pb.Pod{
			{Namespace: "default", Name: "first"},
			{Namespace: "default", Name: "second"},
		},
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("SetDNSChaos() code = %s, want %s", status.Code(err), codes.AlreadyExists)
	}
	if len(chaos.chaosMap) != 0 || len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
		t.Fatal("duplicate pod IPs must not modify chaos state")
	}
}

func TestSetDNSChaosDoesNotCommitAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := fake.NewSimpleClientset(testPod("target", "10.0.0.1"))
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		cancel()
		return false, nil, nil
	})
	chaos := New()
	chaos.Client = client.CoreV1()

	_, err := chaos.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name:   "cancelled-rule",
		Action: ActionError,
		Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
	})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SetDNSChaos() code = %s, want %s", status.Code(err), codes.Canceled)
	}
	if len(chaos.chaosMap) != 0 || len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
		t.Fatal("cancelled request must not modify chaos state")
	}
}

func TestSetDNSChaosRejectsUnsupportedScopeAndSelector(t *testing.T) {
	client := fake.NewSimpleClientset(testPod("target", "10.0.0.1"))
	chaos := New()
	chaos.Client = client.CoreV1()

	for _, request := range []*pb.SetDNSChaosRequest{
		{
			Name:   "scoped-rule",
			Action: ActionError,
			Scope:  "inner",
			Pods:   []*pb.Pod{{Namespace: "default", Name: "target"}},
		},
		{
			Name:     "selected-rule",
			Action:   ActionError,
			Selector: "app=web",
			Pods:     []*pb.Pod{{Namespace: "default", Name: "target"}},
		},
	} {
		_, err := chaos.SetDNSChaos(context.Background(), request)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("SetDNSChaos(%s) code = %s, want %s", request.Name, status.Code(err), codes.InvalidArgument)
		}
		if len(chaos.chaosMap) != 0 || len(chaos.podMap) != 0 || len(chaos.ipPodMap) != 0 {
			t.Fatal("unsupported request fields must not modify chaos state")
		}
	}
}

func testPod(name, ip string) *api.Pod {
	return &api.Pod{
		Namespace: "default", Name: name,
		Status: api.PodStatus{PodIP: ip},
	}
}

func TestLegacyGRPCMethods(t *testing.T) {
	chaos := New()
	chaos.Client = fake.NewSimpleClientset(testPod("target", "10.0.0.1")).CoreV1()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterDNSServer(server, chaos)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response := &pb.DNSChaosResponse{}
	if err := conn.Invoke(ctx, "/pb.DNS/SetDNSChaos", &pb.SetDNSChaosRequest{
		Name: "legacy-rule", Action: ActionError, Pods: []*pb.Pod{{Namespace: "default", Name: "target"}},
	}, response); err != nil {
		t.Fatal(err)
	}
	if !response.Result || chaos.getChaosPod(ctx, "10.0.0.1") == nil {
		t.Fatal("legacy Set must apply rule")
	}
	if err := conn.Invoke(ctx, "/pb.DNS/CancelDNSChaos", &pb.CancelDNSChaosRequest{Name: "legacy-rule"}, response); err != nil {
		t.Fatal(err)
	}
	if !response.Result || chaos.getChaosPod(ctx, "10.0.0.1") != nil {
		t.Fatal("legacy Cancel must remove rule")
	}
}

func TestSetDNSChaosRepeatedTargetUpdatesWithoutDuplicates(t *testing.T) {
	chaos := New()
	chaos.Client = fake.NewSimpleClientset(testPod("target", "10.0.0.1")).CoreV1()
	for _, action := range []string{ActionError, ActionRandom} {
		_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
			Name: "repeated-rule", Action: action, Pods: []*pb.Pod{{Namespace: "default", Name: "target"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	pod := chaos.getChaosPod(context.Background(), "10.0.0.1")
	if pod == nil || pod.Action != ActionRandom || len(chaos.chaosMap["repeated-rule"].pods) != 1 {
		t.Fatalf("repeated target = %#v, want one random target", pod)
	}
}

func TestSetDNSChaosRejectsIPConflictWithinExistingRule(t *testing.T) {
	chaos := New()
	chaos.Client = fake.NewSimpleClientset(testPod("first", "10.0.0.1"), testPod("second", "10.0.0.1")).CoreV1()
	for _, name := range []string{"first", "second"} {
		_, err := chaos.SetDNSChaos(context.Background(), &pb.SetDNSChaosRequest{
			Name: "same-rule", Action: ActionError, Pods: []*pb.Pod{{Namespace: "default", Name: name}},
		})
		if name == "first" && err != nil {
			t.Fatal(err)
		}
		if name == "second" && status.Code(err) != codes.AlreadyExists {
			t.Fatalf("IP conflict code = %s, want AlreadyExists", status.Code(err))
		}
	}
	pod := chaos.getChaosPod(context.Background(), "10.0.0.1")
	if pod == nil || pod.Name != "first" || len(chaos.chaosMap["same-rule"].pods) != 1 {
		t.Fatalf("conflicting update corrupted first target: %#v", pod)
	}
}
