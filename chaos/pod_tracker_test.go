package chaos

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetPodFromClusterWithoutClient(t *testing.T) {
	_, err := New().getPodFromCluster(context.Background(), "default", "test-pod")
	if err == nil {
		t.Fatal("expected an error when the Kubernetes client is not initialized")
	}
}

func TestRefreshPodIPOnce(t *testing.T) {
	const (
		namespace = "default"
		name      = "test-pod"
		podIP     = "10.0.0.1"
	)

	client := fake.NewSimpleClientset(&api.Pod{
		Namespace: namespace, Name: name,
		Status: api.PodStatus{PodIP: podIP},
	})
	var getCalls atomic.Int32
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		getCalls.Add(1)
		return false, nil, nil
	})

	chaos := New()
	chaos.Client = client.CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		IP:             podIP,
		LastUpdateTime: time.Now().Add(-11 * time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[podIP] = podInfo

	var waitGroup sync.WaitGroup
	for range 10 {
		waitGroup.Go(func() {
			chaos.refreshPodIP(context.Background(), podInfo)
		})
	}
	waitGroup.Wait()

	if calls := getCalls.Load(); calls != 1 {
		t.Fatalf("pod refreshes = %d, want 1", calls)
	}
}

func TestGetChaosPodKeepsDeletedPodTargetForRecovery(t *testing.T) {
	const (
		namespace = "default"
		name      = "deleted-pod"
		podIP     = "10.0.0.1"
	)

	chaos := New()
	chaos.Client = fake.NewSimpleClientset().CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		IP:             podIP,
		LastUpdateTime: time.Now().Add(-11 * time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[podIP] = podInfo

	if got := chaos.getChaosPod(context.Background(), podIP); got != nil {
		t.Fatal("deleted pod must not receive chaos")
	}
	if chaos.podMap[namespace][name] != podInfo || podInfo.IP != "" || len(chaos.ipPodMap) != 0 {
		t.Fatal("deleted pod must remain tracked without an active IP mapping")
	}
}

func TestGetChaosPodKeepsPodWithoutIPForRecovery(t *testing.T) {
	const (
		namespace = "default"
		name      = "pending-pod"
		podIP     = "10.0.0.1"
	)

	chaos := New()
	chaos.Client = fake.NewSimpleClientset(&api.Pod{
		Namespace: namespace, Name: name,
	}).CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		IP:             podIP,
		LastUpdateTime: time.Now().Add(-11 * time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[podIP] = podInfo

	if got := chaos.getChaosPod(context.Background(), podIP); got != nil {
		t.Fatal("pod without an IP address must not receive chaos")
	}
	if chaos.podMap[namespace][name] != podInfo || podInfo.IP != "" || len(chaos.ipPodMap) != 0 {
		t.Fatal("pod without an IP must remain tracked without an active IP mapping")
	}
}

func TestGetChaosPodRecoversRecreatedPod(t *testing.T) {
	const (
		namespace = "default"
		name      = "recreated-pod"
		oldIP     = "10.0.0.1"
		newIP     = "10.0.0.2"
	)

	ctx := context.Background()
	client := fake.NewSimpleClientset()
	chaos := New()
	chaos.Client = client.CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		IP:             oldIP,
		LastUpdateTime: time.Now().Add(-podRefreshInterval - time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[oldIP] = podInfo

	if got := chaos.getChaosPod(ctx, oldIP); got != nil {
		t.Fatal("deleted pod must be unbound before recovery")
	}
	_, err := client.CoreV1().Pods(namespace).Create(ctx, &api.Pod{
		Namespace: namespace, Name: name,
		Status: api.PodStatus{PodIP: newIP},
	}, meta.CreateOptions{})
	if err != nil {
		t.Fatalf("create recreated pod: %v", err)
	}

	chaos.Lock()
	podInfo.LastUpdateTime = time.Now().Add(-podRefreshInterval - time.Second)
	chaos.Unlock()

	got := chaos.getChaosPod(ctx, newIP)
	if got == nil || got.IP != newIP {
		t.Fatalf("getChaosPod(%s) = %#v, want recovered pod", newIP, got)
	}
	if chaos.ipPodMap[newIP] != podInfo {
		t.Fatal("recreated pod must be indexed by its new IP")
	}
}

func TestGetChaosPodFindsChangedIPAfterIndexMiss(t *testing.T) {
	const (
		namespace = "default"
		name      = "moved-pod"
		oldIP     = "10.0.0.1"
		newIP     = "10.0.0.2"
	)

	chaos := New()
	chaos.Client = fake.NewSimpleClientset(&api.Pod{
		Namespace: namespace, Name: name,
		Status: api.PodStatus{PodIP: newIP},
	}).CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		Action:         ActionError,
		IP:             oldIP,
		LastUpdateTime: time.Now().Add(-podRefreshInterval - time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[oldIP] = podInfo

	got := chaos.getChaosPod(context.Background(), newIP)
	if got == nil || got.IP != newIP {
		t.Fatalf("getChaosPod(%s) = %#v, want refreshed pod", newIP, got)
	}
	if chaos.ipPodMap[oldIP] != nil || chaos.ipPodMap[newIP] != podInfo {
		t.Fatal("pod IP indexes were not moved atomically")
	}
}

func TestGetChaosPodRejectsRecycledOldIPAfterRefresh(t *testing.T) {
	const (
		namespace = "default"
		name      = "moved-pod"
		oldIP     = "10.0.0.1"
		newIP     = "10.0.0.2"
	)

	chaos := New()
	chaos.Client = fake.NewSimpleClientset(&api.Pod{
		Namespace: namespace, Name: name,
		Status: api.PodStatus{PodIP: newIP},
	}).CoreV1()
	podInfo := &PodInfo{
		Namespace:      namespace,
		Name:           name,
		IP:             oldIP,
		LastUpdateTime: time.Now().Add(-podRefreshInterval - time.Second),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{name: podInfo}
	chaos.ipPodMap[oldIP] = podInfo

	if got := chaos.getChaosPod(context.Background(), oldIP); got != nil {
		t.Fatalf("old IP must not retain chaos after refresh: %#v", got)
	}
	if got := chaos.getChaosPod(context.Background(), newIP); got == nil {
		t.Fatal("new IP must receive the refreshed chaos rule")
	}
}

func TestRefreshPodIPDoesNotOverwriteExistingMapping(t *testing.T) {
	const namespace = "default"

	chaos := New()
	chaos.Client = fake.NewSimpleClientset(&api.Pod{
		Namespace: namespace, Name: "first",
		Status: api.PodStatus{PodIP: "10.0.0.2"},
	}).CoreV1()
	first := &PodInfo{
		Namespace:      namespace,
		Name:           "first",
		IP:             "10.0.0.1",
		LastUpdateTime: time.Now().Add(-podRefreshInterval - time.Second),
	}
	second := &PodInfo{
		Namespace:      namespace,
		Name:           "second",
		IP:             "10.0.0.2",
		LastUpdateTime: time.Now(),
	}
	chaos.podMap[namespace] = map[string]*PodInfo{"first": first, "second": second}
	chaos.ipPodMap[first.IP] = first
	chaos.ipPodMap[second.IP] = second

	chaos.refreshPodIP(context.Background(), first)

	if chaos.ipPodMap["10.0.0.2"] != second {
		t.Fatal("refresh must preserve the existing IP owner")
	}
	if chaos.ipPodMap["10.0.0.1"] != nil || first.IP != "" {
		t.Fatal("conflicting pod must be left tracked but unbound")
	}
}

func TestGetChaosPodDoesNotWaitForRefreshInProgress(t *testing.T) {
	chaos := New()
	pod := &PodInfo{Namespace: "default", Name: "target", IP: "10.0.0.1", Action: ActionError,
		LastUpdateTime: time.Now().Add(-podRefreshInterval - time.Second)}
	chaos.podMap["default"] = map[string]*PodInfo{"target": pod}
	chaos.ipPodMap[pod.IP] = pod
	pod.refreshMu.Lock()
	defer pod.refreshMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan *PodInfo, 1)
	go func() { done <- chaos.getChaosPod(ctx, "10.0.0.1") }()
	select {
	case got := <-done:
		if got == nil || got.Action != ActionError {
			t.Fatalf("lookup during refresh = %#v, want current error rule", got)
		}
	case <-time.After(time.Second):
		t.Fatal("lookup blocked on another refresh despite canceled context")
	}
}
