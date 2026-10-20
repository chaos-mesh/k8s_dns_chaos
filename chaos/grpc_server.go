package chaos

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcServerState struct {
	mu       sync.Mutex
	server   *grpc.Server
	listener net.Listener
}

func (c *DNSChaos) CreateGRPCServer() error {
	c.grpcState.mu.Lock()
	defer c.grpcState.mu.Unlock()
	if c.grpcState.server != nil {
		return nil
	}

	if c.grpcPort == 0 {
		c.grpcPort = 9288
	}
	log.Infof("CreateGRPCServer on port %d", c.grpcPort)

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", c.grpcPort))
	if err != nil {
		return err
	}

	s := grpc.NewServer()
	pb.RegisterDNSServer(s, c.chaosRuntime)
	c.grpcState.server = s
	c.grpcState.listener = grpcListener
	go func(server *grpc.Server, listener net.Listener) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) && !errors.Is(err, net.ErrClosed) {
			log.Errorf("grpc serve error %v", err)
		}

		c.grpcState.mu.Lock()
		if c.grpcState.server == server {
			c.grpcState.server = nil
			c.grpcState.listener = nil
		}
		c.grpcState.mu.Unlock()
		log.Info("grpc server ended")
	}(s, grpcListener)
	log.Info("CreateGRPCServer completed")
	return nil
}

// StopGRPCServer stops the listener and is safe to call more than once.
func (c *DNSChaos) StopGRPCServer() error {
	c.grpcState.mu.Lock()
	defer c.grpcState.mu.Unlock()

	server := c.grpcState.server
	listener := c.grpcState.listener
	if server == nil {
		return nil
	}

	server.Stop()
	c.grpcState.server = nil
	c.grpcState.listener = nil
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	return nil
}

func (c *chaosRuntime) SetDNSChaos(ctx context.Context, req *pb.SetDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive SetDNSChaos request %v", req)

	matcher, err := validateSetDNSChaosRequest(req)
	if err != nil {
		return nil, err
	}

	targets := make([]chaosTarget, 0, len(req.Pods))
	seenIPs := make(map[string]chaosTarget, len(req.Pods))
	for _, pod := range req.Pods {
		v1Pod, err := c.getPodFromCluster(ctx, pod.Namespace, pod.Name)
		if err != nil {
			log.Errorf("failed to getPodFromCluster: %v", err)
			if contextErr := grpcContextError(ctx); contextErr != nil {
				return nil, contextErr
			}
			return nil, err
		}
		if v1Pod == nil {
			return nil, status.Errorf(codes.Internal, "Kubernetes returned an empty pod for %s/%s", pod.Namespace, pod.Name)
		}
		if v1Pod.Status.PodIP == "" {
			return nil, status.Errorf(codes.FailedPrecondition, "pod %s/%s does not have an IP address", pod.Namespace, pod.Name)
		}
		target := chaosTarget{
			namespace: pod.Namespace,
			name:      pod.Name,
			ip:        v1Pod.Status.PodIP,
		}
		if previous, ok := seenIPs[target.ip]; ok {
			return nil, status.Errorf(
				codes.AlreadyExists,
				"pods %s/%s and %s/%s share IP %s",
				previous.namespace,
				previous.name,
				target.namespace,
				target.name,
				target.ip,
			)
		}
		seenIPs[target.ip] = target
		targets = append(targets, target)
	}
	if contextErr := grpcContextError(ctx); contextErr != nil {
		return nil, contextErr
	}

	c.Lock()
	defer c.Unlock()
	if contextErr := grpcContextError(ctx); contextErr != nil {
		return nil, contextErr
	}

	for _, target := range targets {
		if podInfo := c.podMap[target.namespace][target.name]; podInfo != nil && podInfo.RuleName != req.Name {
			return nil, status.Errorf(codes.AlreadyExists, "pod %s/%s is already targeted by chaos rule %q", target.namespace, target.name, podInfo.RuleName)
		}
		if podInfo := c.ipPodMap[target.ip]; podInfo != nil && (podInfo.Namespace != target.namespace || podInfo.Name != target.name) {
			return nil, status.Errorf(codes.AlreadyExists, "pod IP %s is already targeted by chaos rule %q", target.ip, podInfo.RuleName)
		}
	}

	rule := c.chaosMap[req.Name]
	for _, target := range targets {
		if oldPod := c.podMap[target.namespace][target.name]; oldPod != nil {
			c.unbindPodIPLocked(oldPod)
		} else {
			rule.pods = append(rule.pods, chaosPod{namespace: target.namespace, name: target.name})
		}
		if _, ok := c.podMap[target.namespace]; !ok {
			c.podMap[target.namespace] = make(map[string]*PodInfo)
		}

		podInfo := &PodInfo{
			Namespace:      target.namespace,
			Name:           target.name,
			RuleName:       req.Name,
			Action:         req.Action,
			Matcher:        matcher,
			LastUpdateTime: time.Now(),
		}
		if !c.bindPodIPLocked(podInfo, target.ip) {
			return nil, status.Errorf(codes.Internal, "pod IP %s became unavailable while committing chaos rule", target.ip)
		}
		c.podMap[target.namespace][target.name] = podInfo
	}
	c.chaosMap[req.Name] = rule

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}

func (c *chaosRuntime) CancelDNSChaos(ctx context.Context, req *pb.CancelDNSChaosRequest) (*pb.DNSChaosResponse, error) {
	log.Infof("receive CancelDNSChaos request %v", req)
	if req == nil || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "chaos rule name must not be empty")
	}
	if contextErr := grpcContextError(ctx); contextErr != nil {
		return nil, contextErr
	}

	c.Lock()
	defer c.Unlock()
	if contextErr := grpcContextError(ctx); contextErr != nil {
		return nil, contextErr
	}

	_, ok := c.chaosMap[req.Name]
	if !ok {
		return &pb.DNSChaosResponse{
			Result: true,
		}, nil
	}

	c.removeRulePodsLocked(req.Name)
	delete(c.chaosMap, req.Name)

	return &pb.DNSChaosResponse{
		Result: true,
	}, nil
}

type chaosTarget struct {
	namespace string
	name      string
	ip        string
}

type chaosRule struct {
	pods []chaosPod
}

type chaosPod struct {
	namespace string
	name      string
}

func validateSetDNSChaosRequest(req *pb.SetDNSChaosRequest) (*domainMatcher, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "chaos request must not be nil")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "chaos rule name must not be empty")
	}
	if req.Action != ActionError && req.Action != ActionRandom {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported chaos action %q", req.Action)
	}
	if req.Scope != "" {
		return nil, status.Error(codes.InvalidArgument, "scope is not supported")
	}
	if req.Selector != "" {
		return nil, status.Error(codes.InvalidArgument, "selector is not supported")
	}
	if len(req.Pods) == 0 {
		return nil, status.Error(codes.InvalidArgument, "chaos rule must target at least one pod")
	}

	var matcher *domainMatcher
	var err error
	if len(req.Patterns) > 0 {
		matcher, err = newDomainMatcher(req.Patterns)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	seenPods := make(map[string]struct{}, len(req.Pods))
	for _, pod := range req.Pods {
		if pod == nil || pod.Namespace == "" || pod.Name == "" {
			return nil, status.Error(codes.InvalidArgument, "chaos rule contains an invalid pod target")
		}
		podKey := pod.Namespace + "/" + pod.Name
		if _, ok := seenPods[podKey]; ok {
			return nil, status.Errorf(codes.InvalidArgument, "chaos rule contains duplicate pod target %s", podKey)
		}
		seenPods[podKey] = struct{}{}
	}

	return matcher, nil
}

func grpcContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	return nil
}

func (c *chaosRuntime) removeRulePodsLocked(ruleName string) {
	rule, ok := c.chaosMap[ruleName]
	if !ok {
		return
	}

	for _, pod := range rule.pods {
		pods := c.podMap[pod.namespace]
		podInfo := pods[pod.name]
		if podInfo == nil || podInfo.RuleName != ruleName {
			continue
		}

		delete(pods, pod.name)
		c.unbindPodIPLocked(podInfo)
		if len(pods) == 0 {
			delete(c.podMap, pod.namespace)
		}
	}
}
