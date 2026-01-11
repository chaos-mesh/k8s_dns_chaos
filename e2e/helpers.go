package e2e

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
)

const (
	namespace   = "chaos-dns-e2e"
	testPodName = "test-client"
)

func execInPod(podName, namespace, command string) (string, error) {
	cmd := exec.Command("kubectl", "exec", "-n", namespace, podName, "--", "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func resolveDNS(podName, domain string) (string, error) {
	output, err := execInPod(podName, namespace, fmt.Sprintf("nslookup %s 2>&1 || true", domain))
	return output, err
}

func parseNslookupIPs(output string) []string {
	var ips []string
	lines := strings.Split(output, "\n")
	addressSection := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Name:") {
			addressSection = true
			continue
		}
		if addressSection && strings.HasPrefix(line, "Address") {
			idx := strings.Index(line, ":")
			if idx >= 0 && idx+1 < len(line) {
				ip := strings.TrimSpace(line[idx+1:])
				if ip != "" && !strings.Contains(ip, "#") {
					ips = append(ips, ip)
				}
			}
		}
	}

	return ips
}

func isDNSError(output string) bool {
	lowerOutput := strings.ToLower(output)
	return strings.Contains(lowerOutput, "server can't find") ||
		strings.Contains(lowerOutput, "servfail") ||
		strings.Contains(lowerOutput, "connection timed out") ||
		strings.Contains(lowerOutput, "no answer") ||
		strings.Contains(lowerOutput, "nxdomain")
}

func isValidIPv4(ip string) bool {
	ipv4Regex := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	return ipv4Regex.MatchString(ip)
}

func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}
