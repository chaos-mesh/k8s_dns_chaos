module github.com/chaos-mesh/k8s_dns_chaos/e2e

go 1.22

require (
	github.com/chaos-mesh/k8s_dns_chaos v0.0.0
	github.com/stretchr/testify v1.8.4
	google.golang.org/grpc v1.63.2
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/chaos-mesh/k8s_dns_chaos => ../
