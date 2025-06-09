package kubernetes

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	selector "github.com/pingcap/tidb-tools/pkg/table-rule-selector"
	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ScopeInner means chaos only works on the inner host in Kubernetes cluster
	ScopeInner = "inner"
	// ScopeOuter means chaos only works on the outer host of Kubernetes cluster
	ScopeOuter = "outer"
	// ScopeAll means chaos works on all host
	ScopeAll = "all"

	// ActionError means return error for DNS request
	ActionError = "error"
	// ActionRandom means return random IP for DNS request
	ActionRandom = "random"
	// ActionChaos means return chaos IP for DNS request
	ActionStatic = "static"
)

// PodInfo saves some information for pod
type PodInfo struct {
	Namespace      string
	Name           string
	Action         string
	Scope          string
	Selector       selector.Selector
	IP             string
	LastUpdateTime time.Time
}

// IsOverdue ...
func (p *PodInfo) IsOverdue() bool {
	// if the pod's IP is not updated greater than 10 seconds, will treate it as overdue
	// and need to update it
	if time.Since(p.LastUpdateTime) > time.Duration(time.Second*10) {
		return true
	}

	return false
}

func (k Kubernetes) chaosDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg, state request.Request, podInfo *PodInfo) (int, error) {
	if podInfo.Action == ActionError {
		return dns.RcodeServerFailure, fmt.Errorf("dns chaos error")
	}

	answers := []dns.RR{}
	qname := state.Name()

	// TODO: support more type
	switch state.QType() {
	case dns.TypeA:
		ips := []net.IP{getRandomIPv4()}
		log.Debugf("dns.TypeA %v", ips)
		answers = a(qname, 10, ips)
	case dns.TypeAAAA:
		// TODO: return random IP
		ips := []net.IP{net.IP{0x20, 0x1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0x1, 0x23, 0, 0x12, 0, 0x1}}
		log.Debugf("dns.TypeAAAA %v", ips)
		answers = aaaa(qname, 10, ips)
	}

	if len(answers) == 0 {
		return dns.RcodeServerFailure, nil
	}

	log.Infof("answers %v", answers)

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil

}

func getRandomIPv4() net.IP {
	nums := make([]byte, 0, 4)

	for i := 0; i < 4; i++ {
		nums = append(nums, byte(rand.Intn(255)))
	}

	return net.IPv4(nums[0], nums[1], nums[2], nums[3])
}

// a takes a slice of net.IPs and returns a slice of A RRs.
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

// aaaa takes a slice of net.IPs and returns a slice of AAAA RRs.
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

func (k Kubernetes) getChaosPod(ip string) (*PodInfo, error) {
	k.RLock()

	podInfo := k.ipPodMap[ip]
	if podInfo == nil {
		k.RUnlock()
		return nil, nil
	}

	if podInfo.IsOverdue() {
		k.RUnlock()

		v1Pod, err := k.getPodFromCluster(podInfo.Namespace, podInfo.Name)
		if err != nil {
			return nil, err
		}

		if v1Pod.Status.PodIP != podInfo.IP {
			k.Lock()
			podInfo.IP = v1Pod.Status.PodIP
			podInfo.LastUpdateTime = time.Now()

			delete(k.ipPodMap, podInfo.IP)
			k.ipPodMap[v1Pod.Status.PodIP] = podInfo
			k.Unlock()
		}

		return podInfo, nil
	}

	k.RUnlock()
	return podInfo, nil
}

// needChaos judges weather should do chaos for the request
func (k Kubernetes) needChaos(podInfo *PodInfo, records []dns.RR, name string) bool {
	if podInfo == nil {
		return false
	}

	if podInfo.Scope == ScopeAll {
		return true
	}
	if podInfo.Action == ActionStatic {
		domainMap := k.domainIPMapByNamespacedName[podInfo.Namespace][podInfo.Name]
		if domainMap != nil {
			if _, ok := domainMap[name]; ok {
				return true
			}
		}
		return false
	}

	rules := podInfo.Selector.Match(name, "")
	if len(rules) == 0 {
		return false
	}

	match, ok := rules[0].(bool)
	if !ok {
		return false
	}

	return match
}

func (k Kubernetes) getPodFromCluster(namespace, name string) (*api.Pod, error) {
	pods := k.Client.Pods(namespace)
	if pods == nil {
		log.Infof("getPodFromCluster, pods is nil")
		return nil, nil
	}
	return pods.Get(context.Background(), name, meta.GetOptions{})
}

func generateDNSRecords(state request.Request, domainIPMapByNamespacedName map[string]string, r *dns.Msg, w dns.ResponseWriter) (int, error) {
	answers := []dns.RR{}
	qname := state.Name()
	if domainIPMapByNamespacedName == nil {
		return dns.RcodeServerFailure, nil
	}
	ipStr, ok := domainIPMapByNamespacedName[qname]
	if !ok {
		return dns.RcodeServerFailure, fmt.Errorf("domain %s not found", qname)
	}
	ip := net.ParseIP(ipStr)
	switch state.QType() {
	case dns.TypeA:
		ipv4 := ip.To4()
		if ipv4 == nil {
			return dns.RcodeServerFailure, fmt.Errorf("not a valid IPv4 address: %s", ipStr)
		}
		answers = a(qname, 10, []net.IP{ipv4})
		log.Debugf("dns.TypeA %v", ipv4)
	case dns.TypeAAAA:
		ipv6 := ip.To16()
		if ip.To4() != nil {
			return dns.RcodeServerFailure, fmt.Errorf("not a valid IPv6 address: %s", ipStr)
		}
		log.Debugf("dns.TypeAAAA %v", ipv6)
		answers = aaaa(qname, 10, []net.IP{ipv6})
	}
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.Answer = answers

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}
