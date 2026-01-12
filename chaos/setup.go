package chaos

import (
	"strconv"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"k8s.io/client-go/kubernetes"
)

func init() {
	log.Info("init dns_chaos plugin")
	plugin.Register(pluginName, setup)
}

func setup(c *caddy.Controller) error {
	chaos, err := parseConfig(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	err = chaos.initKubeClient()
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		chaos.Next = next
		return chaos
	})

	err = chaos.CreateGRPCServer()
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	return nil
}

func parseConfig(c *caddy.Controller) (*DNSChaos, error) {
	chaos := New()

	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "grpcport":
				args := c.RemainingArgs()
				if len(args) == 1 {
					port, err := strconv.Atoi(args[0])
					if err != nil {
						return nil, err
					}
					chaos.grpcPort = port
				}
			case "kubeconfig":
				args := c.RemainingArgs()
				if len(args) == 2 {
					chaos.kubeconfigPath = args[0]
					chaos.kubeconfigContext = args[1]
				}
			}
		}
	}

	return chaos, nil
}

func (c *DNSChaos) initKubeClient() error {
	config, err := c.getClientConfig()
	if err != nil {
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	c.Client = kubeClient.CoreV1()
	log.Info("kubernetes client initialized")
	return nil
}
