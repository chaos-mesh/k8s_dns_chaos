package chaos

import (
	"context"
	"errors"
	"time"

	api "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	podRefreshInterval = 10 * time.Second
	podRefreshTimeout  = 2 * time.Second
)

func (c *chaosRuntime) getChaosPod(ctx context.Context, ip string) *PodInfo {
	trackedPod, result, isOverdue := c.lookupChaosPod(ip)
	if trackedPod == nil {
		c.refreshOverduePods(ctx)
		_, result, _ = c.lookupChaosPod(ip)
		return result
	}

	if !isOverdue {
		return result
	}

	c.refreshPodIP(ctx, trackedPod)
	_, result, _ = c.lookupChaosPod(ip)
	return result
}

func (c *chaosRuntime) lookupChaosPod(ip string) (*PodInfo, *PodInfo, bool) {
	c.RLock()
	defer c.RUnlock()

	podInfo := c.ipPodMap[ip]
	if podInfo == nil {
		return nil, nil, false
	}

	isOverdue := podInfo.IsOverdue()
	result := &PodInfo{
		Namespace:      podInfo.Namespace,
		Name:           podInfo.Name,
		RuleName:       podInfo.RuleName,
		Action:         podInfo.Action,
		Matcher:        podInfo.Matcher,
		staticIPs:      podInfo.staticIPs,
		IP:             podInfo.IP,
		LastUpdateTime: podInfo.LastUpdateTime,
	}
	return podInfo, result, isOverdue
}

func (c *chaosRuntime) refreshOverduePods(ctx context.Context) {
	if !c.refreshSweepMu.TryLock() {
		return
	}
	defer c.refreshSweepMu.Unlock()

	if !c.lastRefreshSweep.IsZero() && time.Since(c.lastRefreshSweep) < podRefreshInterval {
		return
	}
	c.lastRefreshSweep = time.Now()

	c.RLock()
	pods := make([]*PodInfo, 0, len(c.ipPodMap))
	for _, namespacePods := range c.podMap {
		for _, podInfo := range namespacePods {
			if podInfo.IsOverdue() {
				pods = append(pods, podInfo)
			}
		}
	}
	c.RUnlock()

	sweepCtx, cancel := context.WithTimeout(ctx, podRefreshTimeout)
	defer cancel()
	for _, podInfo := range pods {
		if sweepCtx.Err() != nil {
			return
		}
		c.refreshPodIP(sweepCtx, podInfo)
	}
}

func (c *chaosRuntime) refreshPodIP(ctx context.Context, podInfo *PodInfo) {
	if !podInfo.refreshMu.TryLock() {
		return
	}
	defer podInfo.refreshMu.Unlock()

	c.RLock()
	if !c.isCurrentPodInfoLocked(podInfo) || !podInfo.IsOverdue() {
		c.RUnlock()
		return
	}
	namespace, name := podInfo.Namespace, podInfo.Name
	c.RUnlock()

	refreshCtx, cancel := context.WithTimeout(ctx, podRefreshTimeout)
	defer cancel()
	v1Pod, err := c.getPodFromCluster(refreshCtx, namespace, name)
	if err != nil {
		c.Lock()
		defer c.Unlock()
		if !c.isCurrentPodInfoLocked(podInfo) {
			return
		}
		if apierrors.IsNotFound(err) {
			c.markPodUnavailableLocked(podInfo)
			return
		}
		podInfo.LastUpdateTime = time.Now()
		log.Errorf("failed to refresh pod IP: %v", err)
		return
	}
	if v1Pod.Status.PodIP == "" {
		c.Lock()
		defer c.Unlock()
		if c.isCurrentPodInfoLocked(podInfo) {
			c.markPodUnavailableLocked(podInfo)
		}
		return
	}

	c.Lock()
	defer c.Unlock()

	if !c.isCurrentPodInfoLocked(podInfo) {
		return
	}

	if !c.bindPodIPLocked(podInfo, v1Pod.Status.PodIP) {
		log.Errorf("cannot refresh pod %s/%s to IP %s: IP is already tracked", namespace, name, v1Pod.Status.PodIP)
		c.markPodUnavailableLocked(podInfo)
		return
	}
	podInfo.LastUpdateTime = time.Now()
}

func (c *chaosRuntime) isCurrentPodInfoLocked(podInfo *PodInfo) bool {
	pods, ok := c.podMap[podInfo.Namespace]
	return ok && pods[podInfo.Name] == podInfo
}

func (c *chaosRuntime) bindPodIPLocked(podInfo *PodInfo, ip string) bool {
	if ip == "" {
		return false
	}
	if existing := c.ipPodMap[ip]; existing != nil && existing != podInfo {
		return false
	}

	c.unbindPodIPLocked(podInfo)
	podInfo.IP = ip
	c.ipPodMap[ip] = podInfo
	return true
}

func (c *chaosRuntime) unbindPodIPLocked(podInfo *PodInfo) {
	if c.ipPodMap[podInfo.IP] == podInfo {
		delete(c.ipPodMap, podInfo.IP)
	}
}

func (c *chaosRuntime) markPodUnavailableLocked(podInfo *PodInfo) {
	c.unbindPodIPLocked(podInfo)
	podInfo.IP = ""
	podInfo.LastUpdateTime = time.Now()
}

func (c *chaosRuntime) getPodFromCluster(ctx context.Context, namespace, name string) (*api.Pod, error) {
	if c.Client == nil {
		return nil, errors.New("kubernetes client is not initialized")
	}

	pods := c.Client.Pods(namespace)
	if pods == nil {
		return nil, errors.New("kubernetes pods client is not initialized")
	}
	return pods.Get(ctx, name, meta.GetOptions{})
}
