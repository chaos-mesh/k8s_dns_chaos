package chaos

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	trieselector "github.com/pingcap/tidb-tools/pkg/table-rule-selector"
	"google.golang.org/grpc"
)

// CreateGRPCServer creates and starts the gRPC server
func (c *DNSChaos) CreateGRPCServer() error {
	if c.grpcPort == 0 {
		c.grpcPort = 9288
	}
	log.Infof("CreateGRPCServer on port %d", c.grpcPort)

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", c.grpcPort))
	if err != nil {
		return err
	}

	s := grpc.NewServer()
	pb.RegisterDNSServer(s, c)
	go func() {
		if err := s.Serve(grpcListener); err != nil {
			log.Errorf("grpc serve error %v", err)
		}
		log.Info("grpc server ended")
	}()
	log.Info("CreateGRPCServer completed")
	return nil
}

// SetDNSChaos sets chaos rules for pods
func (c *DNSChaos) SetDNSChaos(ctx context.Context, req *pb.SetDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive SetDNSChaos request %v", req)

	c.Lock()
	defer c.Unlock()

	c.chaosMap[req.Name] = req

	var selector trieselector.Selector
	if len(req.Patterns) > 0 {
		selector = trieselector.NewTrieSelector()
		for _, pattern := range req.Patterns {
			err := selector.Insert(pattern, "", true, trieselector.Insert)
			if err != nil {
				log.Errorf("failed to build selector: %v", err)
				return nil, err
			}

			if !strings.Contains(pattern, "*") {
				err := selector.Insert(fmt.Sprintf("%s.", pattern), "", true, trieselector.Insert)
				if err != nil {
					log.Errorf("failed to build selector: %v", err)
					return nil, err
				}
			}
		}
	}

	for _, pod := range req.Pods {
		v1Pod, err := c.getPodFromCluster(pod.Namespace, pod.Name)
		if err != nil {
			log.Errorf("failed to getPodFromCluster: %v", err)
			return nil, err
		}

		if _, ok := c.podMap[pod.Namespace]; !ok {
			c.podMap[pod.Namespace] = make(map[string]*PodInfo)
		}

		if oldPod, ok := c.podMap[pod.Namespace][pod.Name]; ok {
			delete(c.podMap[pod.Namespace], pod.Name)
			delete(c.ipPodMap, oldPod.IP)
		}

		podInfo := &PodInfo{
			Namespace:      pod.Namespace,
			Name:           pod.Name,
			Action:         req.Action,
			Selector:       selector,
			IP:             v1Pod.Status.PodIP,
			LastUpdateTime: time.Now(),
		}

		c.podMap[pod.Namespace][pod.Name] = podInfo
		c.ipPodMap[v1Pod.Status.PodIP] = podInfo
	}

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}

// CancelDNSChaos removes chaos rules for pods
func (c *DNSChaos) CancelDNSChaos(ctx context.Context, req *pb.CancelDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive CancelDNSChaos request %v", req)
	c.Lock()
	defer c.Unlock()

	if _, ok := c.chaosMap[req.Name]; !ok {
		return &pb.DNSChaosResponse{
			Result: true,
		}, nil
	}

	for _, pod := range c.chaosMap[req.Name].Pods {
		if _, ok := c.podMap[pod.Namespace]; ok {
			if podInfo, ok := c.podMap[pod.Namespace][pod.Name]; ok {
				delete(c.podMap[pod.Namespace], pod.Name)
				delete(c.ipPodMap, podInfo.IP)
			}
		}
	}

	shouldDeleteNs := make([]string, 0, 1)
	for namespace, pods := range c.podMap {
		if len(pods) == 0 {
			shouldDeleteNs = append(shouldDeleteNs, namespace)
		}
	}
	for _, namespace := range shouldDeleteNs {
		delete(c.podMap, namespace)
	}

	delete(c.chaosMap, req.Name)

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}
