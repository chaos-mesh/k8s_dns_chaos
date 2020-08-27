package kubernetes

import (
	"context"
	"net"

	"github.com/coredns/coredns/plugin/kubernetes/pb"
	"google.golang.org/grpc"
	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateGRPCServer ...
func (k Kubernetes) CreateGRPCServer(port string) error {
	log.Info("CreateGRPCServer")
	grpcListener, err := net.Listen("tcp", port)
	if err != nil {
		return err
	}

	s := grpc.NewServer()
	pb.RegisterDNSServer(s, k)
	go func() {
		if err := s.Serve(grpcListener); err != nil {
			log.Errorf("grpc serve error %v", err)
		}
	}()
	log.Info("CreateGRPCServer end")
	return nil
}

// SetDNSChaos ...
func (k Kubernetes) SetDNSChaos(ctx context.Context, req *pb.SetDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive SetDNSChaos request %v", req)
	k.Lock()
	defer k.Unlock()

	k.chaosMap[req.Name] = req
	for _, pod := range req.Pods {
		if _, ok := k.podChaosMap[pod.Namespace]; !ok {
			k.podChaosMap[pod.Namespace] = make(map[string]string)
		}
		k.podChaosMap[pod.Namespace][pod.Name] = req.Mode
	}

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}

// CancelDNSChaos ...
func (k Kubernetes) CancelDNSChaos(ctx context.Context, req *pb.CancelDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive CancelDNSChaos request %v", req)
	k.Lock()
	defer k.Unlock()
	for _, pod := range k.chaosMap[req.Name].Pods {
		if _, ok := k.podChaosMap[pod.Namespace]; ok {
			delete(k.podChaosMap[pod.Namespace], pod.Name)
		}
	}

	shouldDeleteNs := make([]string, 0, 1)
	for namespace, pods := range k.podChaosMap {
		if len(pods) == 0 {
			shouldDeleteNs = append(shouldDeleteNs, namespace)
		}
	}
	for _, namespace := range shouldDeleteNs {
		delete(k.podChaosMap, namespace)
	}

	delete(k.chaosMap, req.Name)

	return nil, nil
}

func (k Kubernetes) getChaosMode(pod *api.Pod) string {
	k.RLock()
	defer k.RUnlock()

	if pod == nil {
		return ""
	}

	if _, ok := k.podChaosMap[pod.Namespace]; ok {
		return k.podChaosMap[pod.Namespace][pod.Name]
	}

	return ""
}

func (k Kubernetes) getChaosPod() ([]api.Pod, error) {
	k.RLock()
	defer k.RUnlock()

	pods := make([]api.Pod, 0, 10)
	for namespace := range k.podChaosMap {
		podList, err := k.Client.Pods(namespace).List(context.Background(), meta.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, pod := range podList.Items {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}
