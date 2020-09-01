package kubernetes

import (
	"context"
	"net"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"google.golang.org/grpc"
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
		v1Pod, err := k.getPodFromCluster(pod.Namespace, pod.Name)
		if err != nil {
			return nil, err
		}

		if _, ok := k.podMap[pod.Namespace]; !ok {
			k.podMap[pod.Namespace] = make(map[string]*PodInfo)
		}

		if oldPod, ok := k.podMap[pod.Namespace][pod.Name]; ok {
			// Pod's IP maybe changed, so delete the old pod info
			delete(k.podMap[pod.Namespace], pod.Name)
			delete(k.ipPodMap, oldPod.IP)
		}

		podInfo := &PodInfo{
			Namespace:      pod.Namespace,
			Name:           pod.Name,
			Mode:           req.Mode,
			Scope:          req.Scope,
			IP:             v1Pod.Status.PodIP,
			LastUpdateTime: time.Now(),
		}

		k.podMap[pod.Namespace][pod.Name] = podInfo
		k.ipPodMap[v1Pod.Status.PodIP] = podInfo
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
		if _, ok := k.podMap[pod.Namespace]; ok {
			if podInfo, ok := k.PodMap[pod.Namespace][pod.Name]; ok {
				delete(k.podMap[pod.Namespace], pod.Name)
				delete(k.ipPodMap, podInfo.IP)
			}
		}
	}

	shouldDeleteNs := make([]string, 0, 1)
	for namespace, pods := range k.podMap {
		if len(pods) == 0 {
			shouldDeleteNs = append(shouldDeleteNs, namespace)
		}
	}
	for _, namespace := range shouldDeleteNs {
		delete(k.podMap, namespace)
	}

	delete(k.chaosMap, req.Name)

	return nil, nil
}
