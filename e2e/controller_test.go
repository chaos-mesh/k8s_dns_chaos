package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type controllerFixture struct {
	targets  [2]string
	control  string
	dnsIP    string
	probeIPs map[string]string
}

func (f controllerFixture) clients() []string {
	return []string{f.targets[0], f.targets[1], f.control}
}

type dnsChaosState struct {
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		Experiment struct {
			ContainerRecords []struct {
				ID    string `json:"id"`
				Phase string `json:"phase"`
			} `json:"containerRecords"`
		} `json:"experiment"`
	} `json:"status"`
}

type lookupResult struct {
	output   string
	exitCode int
}

func controllerKubectl(ctx context.Context, stdin []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--request-timeout=15s", "-n", namespace}, args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return string(output), fmt.Errorf("kubectl %v: %w; %s", args, ctx.Err(), stderr.String())
	}
	if err != nil {
		return string(output), fmt.Errorf("kubectl %v: %w; %s", args, err, stderr.String())
	}
	return string(output), nil
}

func waitController(t *testing.T, description string, check func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var last error
	for {
		last = check(ctx)
		if last == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", description, last)
		case <-time.After(time.Second):
		}
	}
}

func loadControllerFixture(t *testing.T) controllerFixture {
	t.Helper()
	ctx := context.Background()
	output, err := controllerKubectl(ctx, nil, "get", "pods", "-l", "e2e=dns-controller", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		t.Fatal(err)
	}
	var targets, controls []string
	for _, pod := range pods.Items {
		switch pod.Metadata.Labels["role"] {
		case "target":
			targets = append(targets, pod.Metadata.Name)
		case "control":
			controls = append(controls, pod.Metadata.Name)
		}
	}
	if len(pods.Items) != 3 || len(targets) != 2 || len(controls) != 1 {
		t.Fatalf("expected two targets and one control, got %s", output)
	}
	sort.Strings(targets)
	f := controllerFixture{targets: [2]string{targets[0], targets[1]}, control: controls[0], probeIPs: make(map[string]string)}
	serviceIP := func(name string, extra ...string) string {
		t.Helper()
		args := append([]string{"get", "service", name, "-o", "jsonpath={.spec.clusterIP}"}, extra...)
		ip, err := controllerKubectl(ctx, nil, args...)
		if err != nil || net.ParseIP(ip) == nil {
			t.Fatalf("read service %s IP: %q, %v", name, ip, err)
		}
		return ip
	}
	f.dnsIP = serviceIP("chaos-coredns")
	f.probeIPs["kubernetes.default.svc.cluster.local."] = serviceIP("kubernetes", "-n", "default")
	for _, action := range []string{"error", "random"} {
		for _, pod := range f.clients() {
			name := action + "-" + pod
			f.probeIPs[probeDomain(action, pod)] = serviceIP(name)
		}
	}
	return f
}

func probeDomain(action, pod string) string {
	return action + "-" + pod + "." + namespace + ".svc.cluster.local."
}

func controllerLookup(ctx context.Context, pod, domain string, server ...string) (lookupResult, error) {
	const marker = "\nE2E_NSLOOKUP_STATUS="
	args := []string{"exec", pod, "-c", "client", "--", "sh", "-c",
		`nslookup -type=a "$@" 2>&1; result=$?; printf '\nE2E_NSLOOKUP_STATUS=%s\n' "$result"`, "sh", domain}
	output, err := controllerKubectl(ctx, nil, append(args, server...)...)
	if err != nil {
		return lookupResult{}, err
	}
	body, status, found := strings.Cut(output, marker)
	code, parseErr := strconv.Atoi(strings.TrimSpace(status))
	if !found || parseErr != nil {
		return lookupResult{}, fmt.Errorf("missing nslookup exit status for %s: %q", pod, output)
	}
	return lookupResult{output: body, exitCode: code}, nil
}

func checkNormalDNS(ctx context.Context, pod, domain, want string, server ...string) error {
	result, err := controllerLookup(ctx, pod, domain, server...)
	if err != nil {
		return err
	}
	ips := parseNslookupIPs(result.output)
	if result.exitCode != 0 || len(ips) != 1 || ips[0] != want {
		return fmt.Errorf("%s lookup %s: want %s, exit=%d output=%s", pod, domain, want, result.exitCode, result.output)
	}
	return nil
}

func deleteControllerExperiment(ctx context.Context, name string) error {
	_, err := controllerKubectl(ctx, nil, "delete", "dnschaos", name, "--ignore-not-found", "--wait=false")
	return err
}

func TestDNSChaosController(t *testing.T) {
	f := loadControllerFixture(t)
	for _, action := range []string{"error", "random"} {
		if !t.Run(action, func(t *testing.T) { checkControllerLifecycle(t, f, action) }) {
			return
		}
	}
}

func checkControllerLifecycle(t *testing.T, f controllerFixture, action string) {
	t.Helper()
	baseline := make(map[string]string)
	for _, pod := range f.clients() {
		resolver, err := controllerKubectl(context.Background(), nil, "exec", pod, "-c", "client", "--", "cat", "/etc/resolv.conf")
		if err != nil {
			t.Fatal(err)
		}
		baseline[pod] = resolver
		waitController(t, pod+" baseline DNS", func(ctx context.Context) error {
			for _, domain := range []string{probeDomain(action, pod), "kubernetes.default.svc.cluster.local."} {
				if err := checkNormalDNS(ctx, pod, domain, f.probeIPs[domain]); err != nil {
					return err
				}
			}
			return nil
		})
	}

	name := "controller-" + action
	patterns := make([]string, 0, 3)
	for _, pod := range f.clients() {
		patterns = append(patterns, strings.TrimSuffix(probeDomain(action, pod), "."))
	}
	experiment, err := json.Marshal(map[string]any{
		"apiVersion": "chaos-mesh.org/v1alpha1", "kind": "DNSChaos",
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"spec": map[string]any{
			"action": action, "mode": "all", "patterns": patterns,
			"selector": map[string]any{"namespaces": []string{namespace}, "labelSelectors": map[string]string{"e2e": "dns-controller", "role": "target"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controllerKubectl(context.Background(), experiment, "create", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			state, err := controllerKubectl(context.Background(), nil, "get", "dnschaos", name, "-o", "yaml")
			t.Logf("DNSChaos before cleanup: %s; %v", state, err)
		}
		waitController(t, "cleanup experiment deletion", func(ctx context.Context) error {
			if err := deleteControllerExperiment(ctx, name); err != nil {
				return err
			}
			output, err := controllerKubectl(ctx, nil, "get", "dnschaos", name, "--ignore-not-found", "-o", "name")
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "" {
				return fmt.Errorf("experiment still exists: %s", output)
			}
			return nil
		})
	})

	waitController(t, "both targets injected", func(ctx context.Context) error {
		output, err := controllerKubectl(ctx, nil, "get", "dnschaos", name, "-o", "json")
		if err != nil {
			return err
		}
		var state dnsChaosState
		if err := json.Unmarshal([]byte(output), &state); err != nil {
			return err
		}
		injected := false
		for _, condition := range state.Status.Conditions {
			if condition.Type == "AllInjected" && condition.Status == "True" {
				injected = true
			}
		}
		want := map[string]bool{}
		for _, pod := range f.targets {
			want[namespace+"/"+pod+"/client"] = true
		}
		for _, record := range state.Status.Experiment.ContainerRecords {
			if !want[record.ID] || record.Phase != "Injected" {
				return fmt.Errorf("unexpected injection records: %s", output)
			}
			delete(want, record.ID)
		}
		if !injected || len(want) != 0 {
			return fmt.Errorf("injection incomplete: %s", output)
		}
		return nil
	})

	waitController(t, "resolver selection", func(ctx context.Context) error {
		for _, pod := range f.clients() {
			resolver, err := controllerKubectl(ctx, nil, "exec", pod, "-c", "client", "--", "cat", "/etc/resolv.conf")
			if err != nil {
				return err
			}
			if pod == f.control {
				if resolver != baseline[pod] {
					return fmt.Errorf("control resolver changed: %q", resolver)
				}
				continue
			}
			var nameservers []string
			for line := range strings.SplitSeq(resolver, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "nameserver" {
					nameservers = append(nameservers, fields[1])
				}
			}
			if len(nameservers) != 1 || nameservers[0] != f.dnsIP {
				return fmt.Errorf("%s resolver does not use %s: %q", pod, f.dnsIP, resolver)
			}
		}
		return nil
	})

	for _, pod := range f.targets {
		waitController(t, pod+" injected DNS", func(ctx context.Context) error {
			domain := probeDomain(action, pod)
			result, err := controllerLookup(ctx, pod, domain)
			if err != nil {
				return err
			}
			ips := parseNslookupIPs(result.output)
			if action == "error" {
				lower := strings.ToLower(result.output)
				if result.exitCode != 1 || !strings.Contains(lower, "servfail") || strings.Contains(lower, "timed out") || strings.Contains(lower, "nxdomain") || len(ips) != 0 {
					return fmt.Errorf("%s expected SERVFAIL: exit=%d output=%s", pod, result.exitCode, result.output)
				}
			} else {
				if result.exitCode != 0 || len(ips) == 0 {
					return fmt.Errorf("%s expected random IP: exit=%d output=%s", pod, result.exitCode, result.output)
				}
				for _, ip := range ips {
					if net.ParseIP(ip) == nil || ip == f.probeIPs[domain] {
						return fmt.Errorf("%s unexpected random answer: %s", pod, result.output)
					}
				}
			}
			t.Logf("%s %s: exit=%d output=%s", action, pod, result.exitCode, result.output)
			return checkNormalDNS(ctx, pod, "kubernetes.default.svc.cluster.local.", f.probeIPs["kubernetes.default.svc.cluster.local."])
		})
	}
	waitController(t, "control matching DNS", func(ctx context.Context) error {
		domain := probeDomain(action, f.control)
		if err := checkNormalDNS(ctx, f.control, domain, f.probeIPs[domain]); err != nil {
			return err
		}
		return checkNormalDNS(ctx, f.control, domain, f.probeIPs[domain], f.dnsIP)
	})

	if err := deleteControllerExperiment(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	waitController(t, "experiment deletion and recovery", func(ctx context.Context) error {
		output, err := controllerKubectl(ctx, nil, "get", "dnschaos", name, "--ignore-not-found", "-o", "name")
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("experiment still exists: %s", output)
		}
		for _, pod := range f.clients() {
			resolver, err := controllerKubectl(ctx, nil, "exec", pod, "-c", "client", "--", "cat", "/etc/resolv.conf")
			if err != nil {
				return err
			}
			if resolver != baseline[pod] {
				return fmt.Errorf("%s resolver not restored: got %q, want %q", pod, resolver, baseline[pod])
			}
			domain := probeDomain(action, pod)
			if err := checkNormalDNS(ctx, pod, domain, f.probeIPs[domain]); err != nil {
				return err
			}
			if err := checkNormalDNS(ctx, pod, domain, f.probeIPs[domain], f.dnsIP); err != nil {
				return err
			}
		}
		return nil
	})
}
