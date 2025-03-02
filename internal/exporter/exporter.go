package exporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	"github.com/camptocamp/prometheus-puppetdb-exporter/internal/puppetdb"
)

// Exporter implements the prometheus.Exporter interface, and exports PuppetDB metrics
type Exporter struct {
	client    *puppetdb.PuppetDB
	namespace string
	metrics   map[string]*prometheus.GaugeVec
}

var (
	metricMap = map[string]string{
		"node_status_count": "node_status_count",
	}
)

// NewPuppetDBExporter returns a new exporter of PuppetDB metrics.
func NewPuppetDBExporter(url, certPath, caPath, keyPath string, sslSkipVerify bool) (e *Exporter, err error) {
	e = &Exporter{
		namespace: "puppetdb",
	}

	opts := &puppetdb.Options{
		URL:        url,
		CertPath:   certPath,
		CACertPath: caPath,
		KeyPath:    keyPath,
		SSLVerify:  sslSkipVerify,
	}

	e.client, err = puppetdb.NewClient(opts)
	if err != nil {
		log.Fatalf("failed to create new client: %s", err)
		return
	}

	e.initGauges()

	return
}

// Describe outputs PuppetDB metric descriptions
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.metrics {
		m.Describe(ch)
	}
}

// Collect fetches new metrics from the PuppetDB and updates the appropriate metrics
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	for _, m := range e.metrics {
		m.Collect(ch)
	}
}

// Scrape scrapes PuppetDB and update metrics
func (e *Exporter) Scrape(interval time.Duration, unreportedNode string) {
	var statuses map[string]int

	unreportedDuration, err := time.ParseDuration(unreportedNode)
	if err != nil {
		log.Errorf("failed to parse unreported duration: %s", err)
		return
	}

	for {
		statuses = make(map[string]int)

		nodes, err := e.client.Nodes()
		if err != nil {
			log.Errorf("failed to get nodes: %s", err)
		}

		for _, node := range nodes {
			var deactivated string
			if node.Deactivated == "" {
				deactivated = "false"
			} else {
				deactivated = "true"
			}

			if node.ReportTimestamp == "" {
				statuses["unreported"]++
				continue
			}
			latestReport, err := time.Parse("2006-01-02T15:04:05Z", node.ReportTimestamp)
			if err != nil {
				log.Errorf("failed to parse report timestamp: %s", err)
				continue
			}
			e.metrics["report"].With(prometheus.Labels{"environment": node.ReportEnvironment, "host": node.Certname, "deactivated": deactivated}).Set(float64(latestReport.Unix()))

			if latestReport.Add(unreportedDuration).Before(time.Now()) {
				statuses["unreported"]++
			}

			if node.LatestReportStatus != "" {
				statuses[node.LatestReportStatus]++
			} else {
				statuses["unreported"]++
			}

			if node.LatestReportHash != "" {
				reportMetrics, _ := e.client.ReportMetrics(node.LatestReportHash)
				for _, reportMetric := range reportMetrics {
					category := fmt.Sprintf("report_%s", reportMetric.Category)
					e.metrics[category].With(prometheus.Labels{"name": strings.ReplaceAll(strings.Title(reportMetric.Name), "_", " "), "environment": node.ReportEnvironment, "host": node.Certname}).Set(reportMetric.Value)
				}
			}
		}

		for statusName, statusValue := range statuses {
			e.metrics["node_report_status_count"].With(prometheus.Labels{"status": statusName}).Set(float64(statusValue))
		}

		time.Sleep(interval)
	}
}

func (e *Exporter) initGauges() {
	e.metrics = map[string]*prometheus.GaugeVec{}

	e.metrics["node_report_status_count"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: e.namespace,
		Name:      "node_report_status_count",
		Help:      "Total count of reports status by type",
	}, []string{"status"})

	e.metrics["report_resources"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "puppet",
		Name:      "report_resources",
		Help:      "Total count of resources per status",
	}, []string{"name", "environment", "host"})

	e.metrics["report_time"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "puppet",
		Name:      "report_time",
		Help:      "Total execution time per resource type",
	}, []string{"name", "environment", "host"})

	e.metrics["report_changes"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "puppet",
		Name:      "report_changes",
		Help:      "Total count of resources changed",
	}, []string{"name", "environment", "host"})

	e.metrics["report_events"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "puppet",
		Name:      "report_events",
		Help:      "Total count of resources per event",
	}, []string{"name", "environment", "host"})

	e.metrics["report"] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "puppet",
		Name:      "report",
		Help:      "Timestamp of latest report",
	}, []string{"environment", "host", "deactivated"})

	for _, m := range e.metrics {
		prometheus.MustRegister(m)
	}
}
