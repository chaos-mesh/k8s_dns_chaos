package chaos

import (
	"testing"

	"github.com/coredns/caddy"
)

func TestParseConfigGRPCPort(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		wantPort int
		wantErr  bool
	}{
		{
			name:     "minimum valid port",
			input:    "dns_chaos {\n grpcport 1\n}",
			wantPort: 1,
		},
		{
			name:     "maximum valid port",
			input:    "dns_chaos {\n grpcport 65535\n}",
			wantPort: 65535,
		},
		{
			name:    "zero port",
			input:   "dns_chaos {\n grpcport 0\n}",
			wantErr: true,
		},
		{
			name:    "negative port",
			input:   "dns_chaos {\n grpcport -1\n}",
			wantErr: true,
		},
		{
			name:    "port above range",
			input:   "dns_chaos {\n grpcport 65536\n}",
			wantErr: true,
		},
		{
			name:    "grpcport without value",
			input:   "dns_chaos {\n grpcport\n}",
			wantErr: true,
		},
		{
			name:    "grpcport with extra value",
			input:   "dns_chaos {\n grpcport 9288 extra\n}",
			wantErr: true,
		},
		{
			name:    "kubeconfig without context",
			input:   "dns_chaos {\n kubeconfig /path/to/config\n}",
			wantErr: true,
		},
		{
			name:    "unknown option",
			input:   "dns_chaos {\n unknown value\n}",
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			chaos, err := parseConfig(caddy.NewTestController("dns", testCase.input))
			if testCase.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if chaos.grpcPort != testCase.wantPort {
				t.Fatalf("grpcPort = %d, want %d", chaos.grpcPort, testCase.wantPort)
			}
		})
	}
}
