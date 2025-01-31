package mon

// In this file we handle Prometheus metrics about fetching metrics from GCP.

import (
	"strconv"
	"time"

	"github.com/Unity-Technologies/go-lager-internal"
	"github.com/Unity-Technologies/tools-gcp-internal/conn"
	"github.com/prometheus/client_golang/prometheus"
)

type tFirst bool
type tLast bool
type tDelta bool

const isFirst = tFirst(true)
const isLast = tLast(true)
const isDelta = tDelta(true)

var buckets = []float64{
	0.005, 0.01, 0.02, 0.04, 0.08, 0.15, 0.25, 0.5, 1, 2, 4, 8, 15,
}

var mdPageSeconds = NewHistVec(
	"gcpapi", "metric", "desc_page_latency_seconds",
	"Seconds it took to fetch one page of metric descriptors from GCP",
	buckets,
	"project_id", "first_page", "last_page", "code",
)

var tsPageSeconds = NewHistVec(
	"gcpapi", "metric", "value_page_latency_seconds",
	"Seconds it took to fetch one page of metric values from GCP",
	buckets,
	"project_id", "delta", "kind", "first_page", "last_page", "code",
)

var tsCount = NewCounterVec(
	"gcpapi", "metric", "values_total",
	"How many metric values (unique label sets) fetched from GCP",
	"project_id", "delta", "kind",
)

func init() {
	prometheus.MustRegister(mdPageSeconds)
	prometheus.MustRegister(tsPageSeconds)
	prometheus.MustRegister(tsCount)
}

func NewCounterVec(
	system, subsys, name, help string, label_keys ...string,
) *prometheus.CounterVec {
	return prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: system, Subsystem: subsys, Name: name, Help: help,
		},
		label_keys,
	)
}

func NewGaugeVec(
	system, subsys, name, help string, label_keys ...string,
) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: system, Subsystem: subsys, Name: name, Help: help,
		},
		label_keys,
	)
}

func NewHistVec(
	system, subsys, name, help string,
	buckets []float64,
	label_keys ...string,
) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: system, Subsystem: subsys, Name: name, Help: help,
			Buckets: buckets,
		},
		label_keys,
	)
}

func SecondsSince(start time.Time) float64 {
	return float64(time.Now().Sub(start)) / float64(time.Second)
}

func bLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func mdPageSecs(
	start time.Time,
	projectID string,
	isFirstPage tFirst,
	isLastPage tLast,
	pageErr error,
) {
	m, err := mdPageSeconds.GetMetricWithLabelValues(
		projectID,
		bLabel(bool(isFirstPage)),
		bLabel(bool(isLastPage)),
		strconv.Itoa(conn.ErrorCode(pageErr)),
	)
	if nil != err {
		lager.Fail().Map("Can't get mdPageSecs metric for labels", err)
		return
	}
	m.Observe(SecondsSince(start))
}

func tsPageSecs(
	start time.Time,
	projectID string,
	isDelta tDelta,
	kind string,
	isFirstPage tFirst,
	isLastPage tLast,
	pageErr error,
) {
	m, err := tsPageSeconds.GetMetricWithLabelValues(
		projectID,
		bLabel(bool(isDelta)),
		kind,
		bLabel(bool(isFirstPage)),
		bLabel(bool(isLastPage)),
		strconv.Itoa(conn.ErrorCode(pageErr)),
	)
	if nil != err {
		lager.Fail().Map("Can't get tsPageSecs metric for labels", err)
		return
	}
	m.Observe(SecondsSince(start))
}

func tsCountAdd(
	count int,
	projectID string,
	isDelta tDelta,
	kind string,
) {
	m, err := tsCount.GetMetricWithLabelValues(
		projectID,
		bLabel(bool(isDelta)),
		kind,
	)
	if nil != err {
		lager.Fail().Map("Can't get tsCount metric for labels", err)
		return
	}
	m.Add(float64(count))
}
