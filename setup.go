package dynamicrecords

import (
	"fmt"
	"strconv"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

const pluginName = "dynamicrecords"

func init() {
	plugin.Register(pluginName, setup)
}

func setup(c *caddy.Controller) error {
	dr, sharedServer, err := parseDynamicRecords(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		dr.Next = next
		return dr
	})

	// Start shared HTTP server on first instance
	c.OnStartup(func() error {
		return sharedServer.Start()
	})

	// Unregister instance on shutdown
	c.OnShutdown(func() error {
		return sharedServer.Unregister()
	})

	return nil
}

func parseDynamicRecords(c *caddy.Controller) (*DynamicRecords, *SharedServer, error) {
	// Configuration for shared server
	httpAddr := ":8053"
	fstrmAddr := ""
	certFile := ""
	keyFile := ""
	caFile := ""
	defaultTTL := uint32(300)

	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "http_addr":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				httpAddr = c.Val()
			case "fstrm_addr":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				fstrmAddr = c.Val()
			case "cert":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				certFile = c.Val()
			case "key":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				keyFile = c.Val()
			case "ca":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				caFile = c.Val()
			case "default_ttl":
				if !c.NextArg() {
					return nil, nil, c.ArgErr()
				}
				ttl, err := strconv.Atoi(c.Val())
				if err != nil {
					return nil, nil, c.Errf("invalid TTL: %v", err)
				}
				defaultTTL = uint32(ttl)
			default:
				return nil, nil, c.Errf("unknown property '%s'", c.Val())
			}
		}
	}

	// Validate mTLS configuration
	if certFile == "" || keyFile == "" || caFile == "" {
		return nil, nil, fmt.Errorf("cert, key, and ca files are required for mTLS")
	}

	// Get or create the shared server instance
	sharedServer, err := GetOrCreateSharedServer(httpAddr, fstrmAddr, certFile, keyFile, caFile, defaultTTL)
	if err != nil {
		return nil, nil, err
	}

	// Create the plugin instance
	dr := &DynamicRecords{
		sharedServer: sharedServer,
	}

	return dr, sharedServer, nil
}
