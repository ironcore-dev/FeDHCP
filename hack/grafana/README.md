# Grafana dashboard

`fedhcp-dashboard.json` visualizes the FeDHCP stateless pool metrics exposed on
`/metrics` (see the Metrics section in the top-level README).

## Import

1. In Grafana: **Dashboards -> New -> Import**.
2. Upload `fedhcp-dashboard.json` (or paste its contents).
3. Select your Prometheus data source when prompted.

Or via provisioning, drop the file into a provider's path, e.g.
`/etc/grafana/provisioning/dashboards/`.

## Panels

- **Active pools** - relay `/80` prefixes with at least one active lease.
- **Active leases (total)** - sum of `fedhcp_pool_ips_leased`.
- **Assignments (last 1h)** - `increase(fedhcp_leases_total[1h])` (new leases plus renewals).
- **Renewal ratio (1h)** - renewals as a share of all assignments over the last hour.
- **Active leases per pool** - `fedhcp_pool_ips_leased` per prefix.
- **Assignments vs renewals (per hour)** - stacked new-lease and renewal counts over a trailing 1h window.
- **Assignments (per hour)** - `increase(fedhcp_leases_total[1h])` across the selected instance.
- **Pools with an active host** - number of `/80` pools (ports) holding at least one active lease.

Two template variables at the top:
- **FeDHCP instance** - choose **In-band** (`fedhcp`), **OOB** (`fedhcp-oob`), or **All** (matched on the `job` label).
- **Pool (prefix)** - filter to one or more `/80` pools (scoped to the selected instance).

## Try it locally

Point Prometheus at a running FeDHCP instance:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: fedhcp
    static_configs:
      - targets: ["localhost:9090"]
```

Start FeDHCP with its metrics endpoint (enabled by default on `:9090`), add the
Prometheus data source to Grafana, then import the dashboard.
