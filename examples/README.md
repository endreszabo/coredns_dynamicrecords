# CoreDNS DynamicRecords Grafana Dashboard

This directory contains example configurations and dashboards for monitoring the CoreDNS DynamicRecords plugin.

## Grafana Dashboard

### Importing the Dashboard

The `grafana-dashboard.json` file contains a pre-built Grafana dashboard for monitoring the DynamicRecords plugin metrics.

**To import:**

1. Open Grafana
2. Navigate to **Dashboards** → **Import**
3. Click **Upload JSON file**
4. Select `grafana-dashboard.json`
5. Select your Prometheus data source
6. Click **Import**

### Dashboard Panels

The dashboard includes the following panels:

1. **Buffer Lookup Rate (Hit vs Miss)** - Time series showing the rate of buffer hits vs misses
2. **Record Injection Success Rate %** - Gauge showing the overall record injection success percentage
3. **Operations Rate by Type and Protocol** - Time series of add/remove operations split by HTTP and FrameStreams
4. **Error Rate by Operation and Protocol** - Time series showing error rates for each operation type and protocol
5. **Operations Distribution by Protocol** - Pie chart showing the distribution of operations between HTTP and FrameStreams
6. **Operations Distribution by Type** - Pie chart showing the ratio of add vs remove operations
7. **Buffer Lookups by Response Code** - Time series showing lookups grouped by DNS response code (NXDOMAIN, NOERROR, etc.)
8. **All Operations Rate (with Statistics)** - Comprehensive view of all operations with mean, max, and last value statistics
9. **Operations Stacked by Result** - Stacked view showing success vs error rates for add and remove operations

### Metrics Used

The dashboard queries the following Prometheus metrics:

- `coredns_dynamicrecords_buffer_lookup_count_total` - Buffer lookup metrics
- `coredns_dynamicrecords_operations_count_total` - API operation metrics

### Requirements

- Grafana 8.0+ (tested with 10.x)
- Prometheus as a data source
- CoreDNS DynamicRecords plugin with metrics enabled
- CoreDNS `metrics` plugin configured and scraping metrics

### Example Corefile Configuration

```
example.com {
    dynamicrecords {
        http_addr  :8053
        fstrm_addr :8054
        cert /path/to/server.crt
        key  /path/to/server.key
        ca   /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}

# Enable metrics
example.com {
    metrics localhost:9153
}
```

Then configure Prometheus to scrape the metrics endpoint:

```yaml
scrape_configs:
  - job_name: 'coredns'
    static_configs:
      - targets: ['localhost:9153']
```

### Dashboard Variables

The dashboard uses the default Prometheus data source variable `${DS_PROMETHEUS}`. Ensure your Prometheus data source is named or set appropriately in Grafana.

### Customization

You can customize the dashboard by:
- Adjusting the time range (default: last 1 hour)
- Changing the refresh interval (default: 10 seconds)
- Modifying panel queries to suit your specific needs
- Adding alerts based on threshold values
- Adjusting the color schemes and visualization types

### Alerts

Consider setting up alerts for:
- **Low record injection success rate**: If hit rate drops below 70%
- **High error rate**: If error rate exceeds 5% of total operations
- **Sudden drop in operations**: May indicate API connectivity issues

Example alert rule for Prometheus:

```yaml
groups:
  - name: coredns_dynamicrecords
    rules:
      - alert: LowRecordInjectionSuccessRate
        expr: |
          100 * (rate(coredns_dynamicrecords_buffer_lookup_count_total{result="hit"}[5m])
          / (rate(coredns_dynamicrecords_buffer_lookup_count_total{result="hit"}[5m])
          + rate(coredns_dynamicrecords_buffer_lookup_count_total{result="miss"}[5m]))) < 70
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "CoreDNS DynamicRecords low record injection success rate"
          description: "Record injection success rate is below 70% for 10 minutes"

      - alert: HighOperationErrorRate
        expr: |
          100 * (sum(rate(coredns_dynamicrecords_operations_count_total{result="error"}[5m]))
          / sum(rate(coredns_dynamicrecords_operations_count_total[5m]))) > 5
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "CoreDNS DynamicRecords high error rate"
          description: "Operation error rate is above 5% for 5 minutes"
```

## Troubleshooting

### No data showing in dashboard

1. Verify CoreDNS metrics plugin is enabled and running
2. Check Prometheus is scraping the CoreDNS metrics endpoint
3. Confirm the DynamicRecords plugin is processing queries/operations
4. Verify the data source URL in Grafana matches your Prometheus endpoint

### Metrics appear outdated

- Check the dashboard refresh interval
- Verify Prometheus scrape interval is appropriately configured
- Ensure CoreDNS is running and the plugin is active

### Missing metrics

- Ensure you're using the correct metric names
- Check CoreDNS logs for any plugin initialization errors
- Verify the metrics are being exported by querying Prometheus directly:
  ```bash
  curl http://localhost:9153/metrics | grep coredns_dynamicrecords
  ```
