package kubernetes

import (
	"reflect"
	"testing"

	"github.com/chaos-mesh/k8s_dns_chaos/object"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestDefaultProcessor(t *testing.T) {
	pbuild := object.DefaultProcessor(object.ToService(true), nil)
	reh := cache.ResourceEventHandlerFuncs{}
	idx := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})
	processor := pbuild(idx, reh)
	testProcessor(t, processor, idx)
}

func testProcessor(t *testing.T, processor cache.ProcessFunc, idx cache.Indexer) {
	obj := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test1"},
		Spec:       api.ServiceSpec{ClusterIP: "1.2.3.4", Ports: []api.ServicePort{{Port: 80}}},
	}
	obj2 := &api.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test1"},
		Spec:       api.ServiceSpec{ClusterIP: "5.6.7.8", Ports: []api.ServicePort{{Port: 80}}},
	}

	// Add the objects
	err := processor(cache.Deltas{
		{Type: cache.Added, Object: obj},
		{Type: cache.Added, Object: obj2},
	}, false)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	got, exists, err := idx.Get(obj)
	if err != nil {
		t.Fatalf("get added object failed: %v", err)
	}
	if !exists {
		t.Fatal("added object not found in index")
	}
	svc, ok := got.(*object.Service)
	if !ok {
		t.Fatal("object in index was incorrect type")
	}
	if svc.ClusterIP != obj.Spec.ClusterIP {
		t.Fatalf("expected %v, got %v", obj.Spec.ClusterIP, svc.ClusterIP)
	}

	// Update an object
	obj.Spec.ClusterIP = "1.2.3.5"
	err = processor(cache.Deltas{{
		Type:   cache.Updated,
		Object: obj,
	}}, false)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, exists, err = idx.Get(obj)
	if err != nil {
		t.Fatalf("get updated object failed: %v", err)
	}
	if !exists {
		t.Fatal("updated object not found in index")
	}
	svc, ok = got.(*object.Service)
	if !ok {
		t.Fatal("object in index was incorrect type")
	}
	if svc.ClusterIP != obj.Spec.ClusterIP {
		t.Fatalf("expected %v, got %v", obj.Spec.ClusterIP, svc.ClusterIP)
	}

	// Delete an object
	err = processor(cache.Deltas{{
		Type:   cache.Deleted,
		Object: obj2,
	}}, false)
	if err != nil {
		t.Fatalf("delete test failed: %v", err)
	}
	got, exists, err = idx.Get(obj2)
	if err != nil {
		t.Fatalf("get deleted object failed: %v", err)
	}
	if exists {
		t.Fatal("deleted object found in index")
	}

	// Delete an object via tombstone
	key, _ := cache.MetaNamespaceKeyFunc(obj)
	tombstone := cache.DeletedFinalStateUnknown{Key: key, Obj: svc}
	err = processor(cache.Deltas{{
		Type:   cache.Deleted,
		Object: tombstone,
	}}, false)
	if err != nil {
		t.Fatalf("tombstone delete test failed: %v", err)
	}
	got, exists, err = idx.Get(svc)
	if err != nil {
		t.Fatalf("get tombstone deleted object failed: %v", err)
	}
	if exists {
		t.Fatal("tombstone deleted object found in index")
	}
}

func TestDefaultProcessorInitialList(t *testing.T) {
	type addedService struct {
		name    string
		initial bool
	}
	var added []addedService
	handler := cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj interface{}, initial bool) {
			svc := obj.(*object.Service)
			added = append(added, addedService{svc.Name, initial})
		},
	}
	idx := cache.NewIndexer(cache.DeletionHandlingMetaNamespaceKeyFunc, cache.Indexers{})
	processor := object.DefaultProcessor(object.ToService(true), nil)(idx, handler)
	for _, tc := range []struct {
		name    string
		delta   cache.DeltaType
		initial bool
	}{
		{"listed", cache.Sync, true},
		{"watched", cache.Added, false},
	} {
		svc := &api.Service{ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: "test"}}
		if err := processor(cache.Deltas{{Type: tc.delta, Object: svc}}, tc.initial); err != nil {
			t.Fatalf("process %s: %v", tc.name, err)
		}
	}
	want := []addedService{{"listed", true}, {"watched", false}}
	if !reflect.DeepEqual(added, want) {
		t.Fatalf("add events = %#v, want %#v", added, want)
	}
}
