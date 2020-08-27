package kubernetes

import (
	"context"
	"net"

	"github.com/coredns/coredns/plugin/kubernetes/pb"
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
		k.podChaosMap[pod] = req.Mode
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
		delete(k.podChaosMap, pod)
	}

	delete(k.chaosMap, req.Name)

	return nil, nil
}

func (k Kubernetes) getChaosMode(pod string) string {
	k.RLock()
	defer k.RUnlock()

	return k.podChaosMap[pod]
}
