package chaos

import (
	"fmt"
	"sync"

	"github.com/coredns/caddy"
)

type generationKey struct{}

type grpcGeneration struct {
	handler   *DNSChaos
	restartMu sync.Mutex
}

type grpcLifecycleState struct {
	mu         sync.Mutex
	active     *grpcGeneration
	restarting *grpcGeneration
	candidate  *grpcGeneration
}

var grpcLifecycle grpcLifecycleState

func (l *grpcLifecycleState) attach(c *caddy.Controller, handler *DNSChaos) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if existing, ok := c.Get(generationKey{}).(*grpcGeneration); ok {
		if !sameConfig(existing.handler, handler) {
			return fmt.Errorf("dns_chaos directives must use the same gRPC port and Kubernetes configuration")
		}
		handler.chaosRuntime = existing.handler.chaosRuntime
		handler.grpcState = existing.handler.grpcState
		return nil
	}

	if l.active != nil && l.active.handler != nil {
		old := l.active.handler
		if old.kubeconfigPath != handler.kubeconfigPath || old.kubeconfigContext != handler.kubeconfigContext {
			return fmt.Errorf("changing dns_chaos Kubernetes configuration requires a process restart")
		}
		handler.chaosRuntime = old.chaosRuntime
		if old.grpcPort == handler.grpcPort {
			handler.grpcState = old.grpcState
		}
	} else if err := handler.initKubeClient(); err != nil {
		return err
	}

	generation := &grpcGeneration{handler: handler}
	c.Set(generationKey{}, generation)
	c.OnStartup(func() error { return l.prepare(generation) })
	c.OnRestart(func() error { return l.beginRestart(generation) })
	c.OnRestartFailed(func() error { return l.abortRestart(generation) })
	c.OnShutdown(func() error { return l.retire(generation) })
	return nil
}

func sameConfig(a, b *DNSChaos) bool {
	return a.grpcPort == b.grpcPort && a.kubeconfigPath == b.kubeconfigPath && a.kubeconfigContext == b.kubeconfigContext
}

func (l *grpcLifecycleState) prepare(g *grpcGeneration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil && l.restarting != l.active {
		return fmt.Errorf("dns_chaos already belongs to a running CoreDNS instance")
	}
	if err := g.handler.CreateGRPCServer(); err != nil {
		return err
	}
	if l.active == nil {
		l.active = g
	} else {
		l.candidate = g
	}
	return nil
}

func (l *grpcLifecycleState) beginRestart(g *grpcGeneration) error {
	g.restartMu.Lock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != g || l.restarting != nil {
		g.restartMu.Unlock()
		return fmt.Errorf("dns_chaos reload belongs to an obsolete CoreDNS instance")
	}
	l.ensureHook()
	l.restarting = g
	return nil
}

func (l *grpcLifecycleState) abortRestart(g *grpcGeneration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.restarting != g {
		return nil
	}
	candidate := l.candidate
	l.restarting, l.candidate = nil, nil
	g.restartMu.Unlock()
	if candidate != nil && (g.handler == nil || candidate.handler.grpcState != g.handler.grpcState) {
		return candidate.handler.StopGRPCServer()
	}
	return nil
}

func (l *grpcLifecycleState) retire(g *grpcGeneration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != g {
		return nil
	}
	if l.restarting == g {
		l.active = l.candidate
		l.restarting, l.candidate = nil, nil
		g.restartMu.Unlock()
	} else {
		l.active = nil
	}
	if g.handler == nil || (l.active != nil && l.active.handler != nil && l.active.handler.grpcState == g.handler.grpcState) {
		return nil
	}
	return g.handler.StopGRPCServer()
}

const lifecycleHookName = "dns_chaos_lifecycle"

func (l *grpcLifecycleState) ensureHook() {
	for _, name := range caddy.ListPlugins()["event_hooks"] {
		if name == lifecycleHookName {
			return
		}
	}
	caddy.RegisterEventHook(lifecycleHookName, l.instanceStarted)
}

func (l *grpcLifecycleState) instanceStarted(event caddy.EventName, info interface{}) error {
	if event != caddy.InstanceStartupEvent {
		return nil
	}
	instance, ok := info.(*caddy.Instance)
	if !ok || instance.Caddyfile().ServerType() != "dns" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	instance.StorageMu.Lock()
	defer instance.StorageMu.Unlock()
	if instance.Storage[generationKey{}] != nil || l.active != nil {
		return nil
	}
	generation := &grpcGeneration{}
	instance.Storage[generationKey{}] = generation
	l.active = generation
	instance.OnRestart = append(instance.OnRestart, func() error { return l.beginRestart(generation) })
	instance.OnRestartFailed = append(instance.OnRestartFailed, func() error { return l.abortRestart(generation) })
	instance.OnShutdown = append(instance.OnShutdown, func() error { return l.retire(generation) })
	return nil
}
