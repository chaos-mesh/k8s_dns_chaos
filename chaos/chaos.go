package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const pluginName = "dns_chaos"

var log = clog.NewWithPlugin(pluginName)

const (
	ActionError  = "error"
	ActionRandom = "random"
)

type PodInfo struct {
	Namespace      string
	Name           string
	RuleName       string
	Action         string
	Matcher        *domainMatcher
	IP             string
	LastUpdateTime time.Time
	refreshMu      sync.Mutex
}

func (p *PodInfo) IsOverdue() bool {
	return time.Since(p.LastUpdateTime) > podRefreshInterval
}

type DNSChaos struct {
	Next plugin.Handler

	grpcPort          int
	kubeconfigPath    string
	kubeconfigContext string
	grpcState         *grpcServerState
	*chaosRuntime
}

type chaosRuntime struct {
	Client           typev1.CoreV1Interface
	refreshSweepMu   sync.Mutex
	lastRefreshSweep time.Time

	sync.RWMutex
	chaosMap map[string]chaosRule
	podMap   map[string]map[string]*PodInfo
	ipPodMap map[string]*PodInfo
}

func New() *DNSChaos {
	return &DNSChaos{
		grpcPort:  9288,
		grpcState: &grpcServerState{},
		chaosRuntime: &chaosRuntime{
			chaosMap: make(map[string]chaosRule),
			podMap:   make(map[string]map[string]*PodInfo),
			ipPodMap: make(map[string]*PodInfo),
		},
	}
}

func (c *DNSChaos) Name() string { return pluginName }

func (c *DNSChaos) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	sourceIP := state.IP()
	log.Debugf("dns_chaos ServeDNS, source IP: %s, qname: %s", sourceIP, state.QName())

	podInfo := c.getChaosPod(ctx, sourceIP)
	if podInfo == nil {
		return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
	}

	if c.needChaos(podInfo, state.QName()) {
		return c.applyChaos(ctx, w, r, state, podInfo)
	}

	return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
}

func (c *DNSChaos) needChaos(podInfo *PodInfo, qname string) bool {
	if podInfo == nil {
		return false
	}

	if podInfo.Matcher == nil {
		return true
	}

	return podInfo.Matcher.Match(qname)
}

func (c *DNSChaos) applyChaos(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request, podInfo *PodInfo) (int, error) {
	if podInfo.Action == ActionError {
		log.Infof("applying chaos error for pod %s/%s, qname: %s", podInfo.Namespace, podInfo.Name, state.QName())
		return dns.RcodeServerFailure, fmt.Errorf("dns chaos error")
	}

	answers := []dns.RR{}
	qname := state.Name()

	switch state.QType() {
	case dns.TypeA:
		ip := getRandomIPv4()
		log.Infof("applying chaos random IPv4 %v for pod %s/%s, qname: %s", ip, podInfo.Namespace, podInfo.Name, state.QName())
		answers = a(qname, 10, []net.IP{ip})
	case dns.TypeAAAA:
		ip := getRandomIPv6()
		log.Infof("applying chaos random IPv6 %v for pod %s/%s, qname: %s", ip, podInfo.Namespace, podInfo.Name, state.QName())
		answers = aaaa(qname, 10, []net.IP{ip})
	default:
		return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	if err := w.WriteMsg(m); err != nil {
		log.Warningf("failed to write DNS response for pod %s/%s: %v", podInfo.Namespace, podInfo.Name, err)
	}
	return dns.RcodeSuccess, nil
}

func getRandomIPv4() net.IP {
	nums := make([]byte, 4)
	for i := 0; i < 4; i++ {
		nums[i] = byte(rand.Intn(256))
	}
	return net.IPv4(nums[0], nums[1], nums[2], nums[3])
}

func getRandomIPv6() net.IP {
	ip := make([]byte, 16)
	for i := 0; i < 16; i++ {
		ip[i] = byte(rand.Intn(256))
	}
	return net.IP(ip)
}

func a(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl}
		r.A = ip
		answers[i] = r
	}
	return answers
}

func aaaa(zone string, ttl uint32, ips []net.IP) []dns.RR {
	answers := make([]dns.RR, len(ips))
	for i, ip := range ips {
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: zone, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl}
		r.AAAA = ip
		answers[i] = r
	}
	return answers
}

func (c *DNSChaos) getClientConfig() (*rest.Config, error) {
	if c.kubeconfigPath != "" {
		config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.kubeconfigPath},
			&clientcmd.ConfigOverrides{CurrentContext: c.kubeconfigContext},
		)
		return config.ClientConfig()
	}

	return rest.InClusterConfig()
}
