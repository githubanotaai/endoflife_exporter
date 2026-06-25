package collector

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/veerendra2/endoflife_exporter/internal/config"
	"github.com/veerendra2/endoflife_exporter/pkg/endoflife"
)

const (
	endOfLifeProductInfoMetricName                   = "endoflife_product_info"
	endOfLifeLatestVersionTimestampSecondsMetricName = "endoflife_latest_version_timestamp_seconds"
	endOfLifeReleaseCycleTimestampSecondsMetricName  = "endoflife_release_cycle_timestamp_seconds"
	endOfLifeEolFromTimestampSecondsMetricName       = "endoflife_eol_from_timestamp_seconds"
)

var (
	validLabelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

	reservedMetricLabelNames = map[string]struct{}{
		"is_eol":             {},
		"is_lts":             {},
		"is_maintained":      {},
		"latest_version":     {},
		"product_name":       {},
		"release_cycle_name": {},
	}
)

type Exporter struct {
	config                *config.Config
	eolClient             endoflife.Client
	extraMetricLabelNames []string

	productInfoDesc                   *prometheus.Desc
	latestVersionTimestampSecondsDesc *prometheus.Desc
	releaseCycleTimestampSecondsDesc  *prometheus.Desc
	eolFromTimestampSecondsDesc       *prometheus.Desc
}

func NewExporter(cfg config.Config) (*Exporter, error) {
	extraMetricLabelNames, err := customMetricLabelNames(cfg)
	if err != nil {
		return nil, err
	}

	ec, err := endoflife.NewClient()
	if err != nil {
		return nil, err
	}

	return &Exporter{
		config:                &cfg,
		eolClient:             ec,
		extraMetricLabelNames: extraMetricLabelNames,

		productInfoDesc: newMetricDesc(
			endOfLifeProductInfoMetricName,
			"Product release cycle information with EOL status, LTS flag, and maintenance state.",
			extraMetricLabelNames,
			"is_eol",
			"is_lts",
			"is_maintained",
			"latest_version",
			"product_name",
			"release_cycle_name",
		),
		latestVersionTimestampSecondsDesc: newMetricDesc(
			endOfLifeLatestVersionTimestampSecondsMetricName,
			"Release date of the latest version in the release cycle in Unix timestamp.",
			extraMetricLabelNames,
			"product_name",
			"release_cycle_name",
			"latest_version",
		),
		releaseCycleTimestampSecondsDesc: newMetricDesc(
			endOfLifeReleaseCycleTimestampSecondsMetricName,
			"Initial release date of the release cycle in Unix timestamp.",
			extraMetricLabelNames,
			"product_name",
			"release_cycle_name",
		),
		eolFromTimestampSecondsDesc: newMetricDesc(
			endOfLifeEolFromTimestampSecondsMetricName,
			"End-of-life date when the release cycle support ends in Unix timestamp.",
			extraMetricLabelNames,
			"product_name",
			"release_cycle_name",
		),
	}, nil
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.productInfoDesc
	ch <- e.latestVersionTimestampSecondsDesc
	ch <- e.releaseCycleTimestampSecondsDesc
	ch <- e.eolFromTimestampSecondsDesc
}

func customMetricLabelNames(cfg config.Config) ([]string, error) {
	labelNames := map[string]struct{}{}

	for _, product := range cfg.Products {
		for labelName := range product.Labels {
			if err := validateCustomMetricLabelName(labelName); err != nil {
				return nil, fmt.Errorf("invalid label %q for product %q: %w", labelName, product.Name, err)
			}
			labelNames[labelName] = struct{}{}
		}
	}

	labels := make([]string, 0, len(labelNames))
	for labelName := range labelNames {
		labels = append(labels, labelName)
	}
	sort.Strings(labels)

	return labels, nil
}

func validateCustomMetricLabelName(labelName string) error {
	if strings.HasPrefix(labelName, "__") {
		return fmt.Errorf("labels starting with __ are reserved by Prometheus")
	}

	if _, ok := reservedMetricLabelNames[labelName]; ok {
		return fmt.Errorf("label conflicts with an internal metric label")
	}

	if !validLabelName.MatchString(labelName) {
		return fmt.Errorf("label must match [a-zA-Z_][a-zA-Z0-9_]*")
	}

	return nil
}

func newMetricDesc(name string, help string, extraMetricLabelNames []string, baseLabels ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, metricLabelNames(extraMetricLabelNames, baseLabels...), nil)
}

func metricLabelNames(extraMetricLabelNames []string, baseLabels ...string) []string {
	labels := make([]string, 0, len(baseLabels)+len(extraMetricLabelNames))
	labels = append(labels, baseLabels...)
	labels = append(labels, extraMetricLabelNames...)
	return labels
}

func metricLabelValues(product config.Product, extraMetricLabelNames []string, baseValues ...string) []string {
	values := make([]string, 0, len(baseValues)+len(extraMetricLabelNames))
	values = append(values, baseValues...)
	for _, labelName := range extraMetricLabelNames {
		values = append(values, product.Labels[labelName])
	}
	return values
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, product := range e.config.Products {
		var releases []endoflife.ReleaseDetails
		var err error

		if product.AllReleases {
			// Fetch all release cycles for the product
			releases, err = e.eolClient.GetProductDetails(ctx, product.Name)
			if err != nil {
				slog.Error("Failed to get all release cycles", "product_name", product.Name, "error", err)
				continue
			}
		} else {
			// Fetch specific releases
			for _, releaseName := range product.Releases {
				relInfo, err := e.eolClient.GetRelease(ctx, product.Name, releaseName)
				if err != nil {
					slog.Error("Failed to get release cycle", "product_name", product.Name, "release_name", releaseName, "error", err)
					continue
				}
				releases = append(releases, relInfo)
			}
		}

		// Process and export metrics for all releases
		for _, relInfo := range releases {
			ch <- prometheus.MustNewConstMetric(
				e.productInfoDesc,
				prometheus.GaugeValue,
				1,
				metricLabelValues(
					product,
					e.extraMetricLabelNames,
					strconv.FormatBool(relInfo.IsEol),
					strconv.FormatBool(relInfo.IsLts),
					strconv.FormatBool(relInfo.IsMaintained),
					relInfo.LatestVersion,
					product.Name,
					relInfo.ReleaseCycleName,
				)...,
			)

			ch <- prometheus.MustNewConstMetric(
				e.latestVersionTimestampSecondsDesc,
				prometheus.GaugeValue,
				float64(relInfo.LatestVersionDate.Unix()),
				metricLabelValues(
					product,
					e.extraMetricLabelNames,
					product.Name,
					relInfo.ReleaseCycleName,
					relInfo.LatestVersion,
				)...,
			)

			ch <- prometheus.MustNewConstMetric(
				e.releaseCycleTimestampSecondsDesc,
				prometheus.GaugeValue,
				float64(relInfo.ReleaseCycleDate.Unix()),
				metricLabelValues(
					product,
					e.extraMetricLabelNames,
					product.Name,
					relInfo.ReleaseCycleName,
				)...,
			)

			ch <- prometheus.MustNewConstMetric(
				e.eolFromTimestampSecondsDesc,
				prometheus.GaugeValue,
				float64(relInfo.EOLFrom.Unix()),
				metricLabelValues(
					product,
					e.extraMetricLabelNames,
					product.Name,
					relInfo.ReleaseCycleName,
				)...,
			)
		}
	}
}
