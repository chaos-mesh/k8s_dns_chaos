package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chaos-mesh/k8s_dns_chaos/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const grpcAddress = "localhost:9288"

func getGRPCClient(t *testing.T) (pb.DNSClient, *grpc.ClientConn) {
	conn, err := grpc.Dial(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "Failed to connect to gRPC server")
	return pb.NewDNSClient(conn), conn
}

func TestBaselineDNS(t *testing.T) {
	output, err := resolveDNS(testPodName, "kubernetes.default.svc.cluster.local")
	require.NoError(t, err, "Failed to execute nslookup")

	t.Logf("DNS output: %s", output)
	ips := parseNslookupIPs(output)
	assert.NotEmpty(t, ips, "Expected DNS to resolve kubernetes.default service")
}

func TestRandomChaos(t *testing.T) {
	client, conn := getGRPCClient(t)
	defer conn.Close()

	ctx := context.Background()

	resp, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name:   "test-random-chaos",
		Action: "random",
		Pods: []*pb.Pod{
			{Namespace: namespace, Name: testPodName},
		},
	})
	require.NoError(t, err, "Failed to set random chaos")
	require.True(t, resp.Result, "SetDNSChaos should return success")

	time.Sleep(2 * time.Second)

	output, err := resolveDNS(testPodName, "random-test.example.com")
	require.NoError(t, err, "Failed to execute nslookup")
	t.Logf("Random chaos DNS output: %s", output)

	ips := parseNslookupIPs(output)
	assert.NotEmpty(t, ips, "Expected random chaos to return an IP")
	for _, ip := range ips {
		assert.True(t, isValidIP(ip), "Expected a valid IP address (IPv4 or IPv6), got: %s", ip)
	}

	resp, err = client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{
		Name: "test-random-chaos",
	})
	require.NoError(t, err, "Failed to cancel chaos")
	require.True(t, resp.Result, "CancelDNSChaos should return success")
}

func TestErrorChaos(t *testing.T) {
	client, conn := getGRPCClient(t)
	defer conn.Close()

	ctx := context.Background()

	resp, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name:   "test-error-chaos",
		Action: "error",
		Pods: []*pb.Pod{
			{Namespace: namespace, Name: testPodName},
		},
	})
	require.NoError(t, err, "Failed to set error chaos")
	require.True(t, resp.Result, "SetDNSChaos should return success")

	time.Sleep(2 * time.Second)

	output, err := resolveDNS(testPodName, "error-test.example.com")
	require.NoError(t, err, "Failed to execute nslookup")
	t.Logf("Error chaos DNS output: %s", output)

	assert.True(t, isDNSError(output), "Expected DNS to fail with error chaos, got: %s", output)

	resp, err = client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{
		Name: "test-error-chaos",
	})
	require.NoError(t, err, "Failed to cancel chaos")
	require.True(t, resp.Result, "CancelDNSChaos should return success")
}

func TestPatternChaos(t *testing.T) {
	client, conn := getGRPCClient(t)
	defer conn.Close()

	ctx := context.Background()

	resp, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name:     "test-pattern-chaos",
		Action:   "error",
		Patterns: []string{"chaos-test.local*"},
		Pods: []*pb.Pod{
			{Namespace: namespace, Name: testPodName},
		},
	})
	require.NoError(t, err, "Failed to set pattern chaos")
	require.True(t, resp.Result, "SetDNSChaos should return success")

	time.Sleep(2 * time.Second)

	output1, err := resolveDNS(testPodName, "chaos-test.local.foo")
	require.NoError(t, err, "Failed to execute nslookup for matching pattern")
	t.Logf("Pattern chaos (matching) DNS output: %s", output1)
	assert.True(t, isDNSError(output1), "Expected DNS to fail for matching pattern, got: %s", output1)

	output2, err := resolveDNS(testPodName, "kubernetes.default.svc.cluster.local")
	require.NoError(t, err, "Failed to execute nslookup for non-matching pattern")
	t.Logf("Pattern chaos (non-matching) DNS output: %s", output2)
	ips := parseNslookupIPs(output2)
	assert.NotEmpty(t, ips, "Expected DNS to resolve for non-matching pattern")

	resp, err = client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{
		Name: "test-pattern-chaos",
	})
	require.NoError(t, err, "Failed to cancel chaos")
	require.True(t, resp.Result, "CancelDNSChaos should return success")
}

func TestCancelChaos(t *testing.T) {
	client, conn := getGRPCClient(t)
	defer conn.Close()

	ctx := context.Background()

	resp, err := client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
		Name:   "test-cancel-chaos",
		Action: "error",
		Pods: []*pb.Pod{
			{Namespace: namespace, Name: testPodName},
		},
	})
	require.NoError(t, err, "Failed to set chaos")
	require.True(t, resp.Result, "SetDNSChaos should return success")

	time.Sleep(2 * time.Second)

	output1, err := resolveDNS(testPodName, "cancel-test.example.com")
	require.NoError(t, err)
	t.Logf("Before cancel DNS output: %s", output1)
	assert.True(t, isDNSError(output1), "Expected DNS to fail before cancel")

	resp, err = client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{
		Name: "test-cancel-chaos",
	})
	require.NoError(t, err, "Failed to cancel chaos")
	require.True(t, resp.Result, "CancelDNSChaos should return success")

	time.Sleep(2 * time.Second)

	output2, err := resolveDNS(testPodName, "kubernetes.default.svc.cluster.local")
	require.NoError(t, err)
	t.Logf("After cancel DNS output: %s", output2)
	ips := parseNslookupIPs(output2)
	assert.NotEmpty(t, ips, "Expected DNS to resolve after cancel")
}
