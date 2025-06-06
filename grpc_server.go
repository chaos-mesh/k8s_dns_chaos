package kubernetes

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

// CreateGRPCServer ...
func (k Kubernetes) CreateGRPCServer() error {
	if k.grpcPort == 0 {
		// use default port
		k.grpcPort = 9288

	}
	log.Infof("CreateGRPCServer on port %d", k.grpcPort)

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", k.grpcPort))
	if err != nil {
		return err
	}

	s := grpc.NewServer()
	pb.RegisterDNSServer(s, k)
	go func() {
		if err := s.Serve(grpcListener); err != nil {
			log.Errorf("grpc serve error %v", err)
		}
		log.Info("grpc server end")
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

	var scope string
	if len(req.Patterns) == 0 {
		scope = ScopeAll
	}

	// build selector
	selector := trieselector.NewTrieSelector()
	for _, pattern := range req.Patterns {
		err := selector.Insert(pattern, "", true, trieselector.Insert)
		if err != nil {
			log.Errorf("fail to build selector %v", err)
			return nil, err
		}

		if !strings.Contains(pattern, "*") {
			// when send dns request to the dns server, will add a '.' at the end of the domain name.
			err := selector.Insert(fmt.Sprintf("%s.", pattern), "", true, trieselector.Insert)
			if err != nil {
				log.Errorf("fail to build selector %v", err)
				return nil, err
			}
		}
	}
	if req.Action == ActionStatic && req.IpDomainMaps != nil {
		for _, domainIPMap := range req.IpDomainMaps {
			err := selector.Insert(domainIPMap.Domain, "", true, trieselector.Insert)
			if err != nil {
				log.Errorf("fail to build selector %v", err)
				return nil, err
			}
		}
	}

	for _, pod := range req.Pods {
		v1Pod, err := k.getPodFromCluster(pod.Namespace, pod.Name)
		if err != nil {
			log.Errorf("fail to getPodFromCluster %v", err)
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
			Action:         req.Action,
			Scope:          scope,
			Selector:       selector,
			IP:             v1Pod.Status.PodIP,
			LastUpdateTime: time.Now(),
		}

		k.podMap[pod.Namespace][pod.Name] = podInfo
		k.ipPodMap[v1Pod.Status.PodIP] = podInfo
		domainIPMap := saveDomainAndIp(req.IpDomainMaps)
		if domainIPMap != nil {
			if _, ok := k.domainIPMapByNamespacedName[pod.Namespace]; !ok {
				k.domainIPMapByNamespacedName[pod.Namespace] = make(map[string]map[string]string)
			}
			k.domainIPMapByNamespacedName[pod.Namespace][pod.Name] = domainIPMap
		}

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

	if _, ok := k.chaosMap[req.Name]; !ok {
		return &pb.DNSChaosResponse{
			Result: true,
		}, nil
	}

	for _, pod := range k.chaosMap[req.Name].Pods {
		if _, ok := k.podMap[pod.Namespace]; ok {
			if podInfo, ok := k.podMap[pod.Namespace][pod.Name]; ok {
				delete(k.podMap[pod.Namespace], pod.Name)
				delete(k.ipPodMap, podInfo.IP)
			}
			if _, ok1 := k.domainIPMapByNamespacedName[pod.Namespace][pod.Name]; ok1 {
				delete(k.domainIPMapByNamespacedName[pod.Namespace], pod.Name)
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
		delete(k.domainIPMapByNamespacedName, namespace)
	}

	delete(k.chaosMap, req.Name)

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}

// save domain and ip
func saveDomainAndIp(domainMapList []*pb.IpDomainMap) map[string]string {
	if len(domainMapList) == 0 {
		return nil
	}
	domainIPMap := make(map[string]string)
	for _, domainMap := range domainMapList {
		key := fmt.Sprintf("%s.", domainMap.Domain)
		domainIPMap[key] = domainMap.Ip
	}
	return domainIPMap
}
