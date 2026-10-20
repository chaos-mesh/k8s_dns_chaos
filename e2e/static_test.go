package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/stretchr/testify/require"
)

func TestStaticChaos(t *testing.T) {
	client, conn := getGRPCClient(t)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := fmt.Sprintf("static-%d", time.Now().UnixNano())
	domain := "kubernetes.default.svc.cluster.local"
	before, err := resolveDNS(testPodName, domain)
	require.NoError(t, err)
	baseline := parseNslookupIPs(before)
	require.NotEmpty(t, baseline)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, err := client.CancelDNSChaos(cleanupCtx, &pb.CancelDNSChaosRequest{Name: name})
		require.NoError(t, err)
	})
	response, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name: name, Action: "static", Pods: []*pb.Pod{{Namespace: namespace, Name: testPodName}},
		IpDomainMaps: []*pb.IpDomainMap{
			{Domain: domain, Ip: "192.0.2.42"},
			{Domain: "static-v6.example", Ip: "2001:db8::42"},
		},
	})
	require.NoError(t, err)
	require.True(t, response.Result)
	for _, tc := range []struct{ domain, qtype, ip string }{
		{domain, "A", "192.0.2.42"},
		{"static-v6.example", "AAAA", "2001:db8::42"},
	} {
		output, err := execInPod(testPodName, namespace, fmt.Sprintf("nslookup -type=%s %s", tc.qtype, tc.domain))
		require.NoError(t, err, output)
		require.Equal(t, []string{tc.ip}, parseNslookupIPs(output), output)
	}
	fqdn, err := resolveDNS(testPodName, "kubernetes.default.svc.cluster.local.")
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.42"}, parseNslookupIPs(fqdn), fqdn)
	_, err = client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{Name: name})
	require.NoError(t, err)
	after, err := resolveDNS(testPodName, domain)
	require.NoError(t, err)
	require.ElementsMatch(t, baseline, parseNslookupIPs(after), after)
}
