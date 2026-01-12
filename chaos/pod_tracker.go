package chaos

import (
	"context"
	"time"

	api "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getChaosPod looks up pod info by source IP and returns a copy to avoid race conditions
func (c *DNSChaos) getChaosPod(ip string) *PodInfo {
	c.RLock()
	podInfo := c.ipPodMap[ip]
	if podInfo == nil {
		c.RUnlock()
		return nil
	}

	isOverdue := podInfo.IsOverdue()

	// Return a copy to avoid race condition where another goroutine
	// could delete the original from maps while caller is using it
	result := &PodInfo{
		Namespace:      podInfo.Namespace,
		Name:           podInfo.Name,
		Action:         podInfo.Action,
		Selector:       podInfo.Selector,
		IP:             podInfo.IP,
		LastUpdateTime: podInfo.LastUpdateTime,
	}
	c.RUnlock()

	if isOverdue {
		c.refreshPodIP(podInfo)
	}

	return result
}

// refreshPodIP refreshes the pod IP from the cluster
func (c *DNSChaos) refreshPodIP(podInfo *PodInfo) {
	v1Pod, err := c.getPodFromCluster(podInfo.Namespace, podInfo.Name)
	if err != nil {
		log.Errorf("failed to refresh pod IP: %v", err)
		return
	}

	c.Lock()
	defer c.Unlock()

	if v1Pod.Status.PodIP != podInfo.IP {
		delete(c.ipPodMap, podInfo.IP)
		podInfo.IP = v1Pod.Status.PodIP
		c.ipPodMap[v1Pod.Status.PodIP] = podInfo
	}
	podInfo.LastUpdateTime = time.Now()
}

// getPodFromCluster fetches pod info from kubernetes
func (c *DNSChaos) getPodFromCluster(namespace, name string) (*api.Pod, error) {
	pods := c.Client.Pods(namespace)
	if pods == nil {
		log.Infof("getPodFromCluster: pods interface is nil")
		return nil, nil
	}
	return pods.Get(context.Background(), name, meta.GetOptions{})
}
