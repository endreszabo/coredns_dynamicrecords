package dynamicrecords

import (
	"sync"

	"github.com/coredns/coredns/plugin"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricsOnce       sync.Once
	bufferLookupCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: plugin.Namespace,
			Subsystem: pluginName,
			Name:      "buffer_lookup_count_total",
			Help:      "Number of buffer lookups for DNS query responses, labeled by hit/miss result and downstream rcode",
		},
		[]string{"result", "rcode"},
	)

	operationsCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: plugin.Namespace,
			Subsystem: pluginName,
			Name:      "operations_count_total",
			Help:      "Number of add/remove operations via the API, labeled by operation type, transport protocol, and result",
		},
		[]string{"operation", "protocol", "result"},
	)
)

func init() {
	prometheus.MustRegister(bufferLookupCount)
	prometheus.MustRegister(operationsCount)
}
