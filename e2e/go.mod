module github.com/chaos-mesh/k8s_dns_chaos/e2e

go 1.25

require (
	github.com/chaos-mesh/k8s_dns_chaos v0.0.0
	github.com/stretchr/testify v1.8.4
	google.golang.org/grpc v1.56.3
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/chaos-mesh/k8s_dns_chaos => ../
