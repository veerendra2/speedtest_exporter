# Development environment

Exporter + VictoriaMetrics + Prometheus + Grafana, so you can see the metrics
and both dashboards without setting anything up by hand.

```bash
# from the repository root
docker compose -f development/compose.yml up --build -d
```

| What           | Where                             |
| -------------- | --------------------------------- |
| Exporter       | http://localhost:8080/metrics     |
| Probe          | http://localhost:8080/probe?target=nearest |
| VictoriaMetrics| http://localhost:8428/vmui        |
| Prometheus     | http://localhost:9090             |
| Grafana        | http://localhost:3001 (admin / changeme) |

Both dashboards are provisioned automatically: **Dashboards → Speedtest Exporter
(VictoriaMetrics)** and **(Prometheus)**. They are the same dashboard against the
two datasources; VictoriaMetrics is the default.

The scrapers start immediately, but a probe takes 30–60 seconds and Prometheus
spreads first scrapes over its interval, so the panels say "No data" for the
first few minutes. The dashboards default to the last 24 hours and refresh every
5 minutes.

A probe takes 30–60 seconds — it runs a real speedtest against speedtest.net,
so it needs internet access and **moves a few hundred MB every time**. Both
VictoriaMetrics and Prometheus scrape the same two targets (`nearest` and
`berlin`) every 5 minutes, so the stack runs four probes per interval. Drop one
of the two scrapers in `development/compose.yml` if that is too much; lower
`scrape_interval` in `development/compose.yml` if you want the charts to fill
faster, and remember it costs bandwidth.

## What gets scraped

```yaml
- job_name: speedtest_exporter
  metrics_path: /probe
  scrape_interval: 5m
  scrape_timeout: 2m
  params:
    target: ["nearest"]
  static_configs:
    - targets: ["speedtest_exporter:8080"]
  relabel_configs:
    - source_labels: [__param_target]
      target_label: instance
```

`target` is a name you choose; it becomes the `target` label on every metric.
`instance` is relabelled from it, because every probe comes from the same
exporter address.

## Adding a second target

Each target is a separate scrape config. Give each one its own `job_name` so
the results stay apart:

```yaml
- job_name: speedtest_berlin
  metrics_path: /probe
  scrape_interval: 5m
  scrape_timeout: 2m
  params:
    target: ["berlin"]
    server_id: ["17982"]
  static_configs:
    - targets: ["speedtest_exporter:8080"]
  relabel_configs:
    - source_labels: [__param_target]
      target_label: instance
```

Probes are serialised: a second probe that arrives while one is running waits,
and if it waits past the 2-minute probe timeout it reports `speedtest_status 0`
instead of measuring. Stagger the intervals if you add several targets.

## Editing the dashboard

The JSON in `../assets/` is provisioned read-only. Changes to it need a Grafana
restart to show up (the v2 schema provisioner does not hot-reload), and edits
made in the UI are overwritten on the next provision:

```bash
docker compose -f development/compose.yml restart grafana
```

## Tear down

```bash
docker compose -f development/compose.yml down          # keep data
docker compose -f development/compose.yml down -v       # drop volumes
```
