module github.com/chaos-mesh/k8s_dns_chaos/e2e

go 1.25

require (
	github.com/chaos-mesh/k8s_dns_chaos v0.0.0
	github.com/stretchr/testify v1.8.4
	google.golang.org/grpc v1.78.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/chaos-mesh/k8s_dns_chaos => ../
