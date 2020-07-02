package config

import (
	"os"
	"strings"

	"github.com/TyeMcQueen/tools-gcp/mon"
	"github.com/TyeMcQueen/go-lager"
	"google.golang.org/api/monitoring/v3"
	"gopkg.in/yaml.v2"
)


//// Global Data Types ////

// This type specifies what data can be put in the gcp2prom.yaml
// configuration file to control which GCP metrics can be exported to
// Prometheus and to configure how each gets converted.
//
// Note that YAML lowercases the letters of keys so, for example, to set
// MaxBuckets, your YAML would contain something like `maxbuckets: 32`.
type config struct {
	// System is the first part of each Prometheus metric name.
	System      string

	// Subsystem maps GCP metric path prefixes to the 2nd part of Prom metric
	// names.  Each key should be a prefix of some GCP metric names (paths) up
	// to (and including) a '/' character (for example,
	// "bigquery.googleapis.com/query/").  Metrics that do not match any of
	// these prefixes are silently ignored.
	//
	// Only the longest matching prefix is applied (per metric).
	Subsystem   map[string]string

	// Units maps a unit name to the name of a predefined scaling factor to
	// convert to base units that are preferred in Prometheus.  The names
	// of the conversion functions look like multiplication or division
	// operations, often with repeated parts.  For example,
	// `"ns": "/1000/1000/1000"` to convert nanoseconds to seconds and
	// `"d": "*60*60*24"` to convert days to seconds.
	//
	// Each key is a string containing a comma-separated list of unit types.
	// If you use the same unit type in multiple entries, then which of those
	// entries that will be applied to a metric will be "random".
	Units       map[string]string

	// Suffix adjusts the last part of Prometheus
	// metric names by replacing a suffix.
	//
	// For each section, only the longest matching suffix is applied.
	Suffix      struct {
		Any         map[string]string
		Histogram   map[string]string
		Counter     map[string]string
		Gauge       map[string]string
	}

	// Histogram is a mapping ...
	Histogram   map[string]struct {
		// MinBound specifies the minimum allowed bucket boundary.  If the
		// first bucket boundary is below this value, then the lowest
		// bucket boundary that is at least MinBound becomes the first
		// bucket boundary (and the counts for all prior buckets get
		// combined into the new first bucket).  Since metric values can
		// be negative, a MinBound of 0.0 is not ignored (unless MaxBound
		// is also 0.0).
		MinBound,
		// MinRatio specifies the minimum ratio between adjacent bucket
		// boundaries.  If the ratio between two adjacent bucket boundaries
		// (larger boundary divided by smaller boundary) is below MinRatio,
		// then the larger boundary (at least) will be removed, combining the
		// counts from at least two buckets into a new, wider bucket.
		//
		// If MinRatio is 0.0, then no minimum is applied.  Specifying a
		// MinRatio at or below 1.0 that is not 0.0 is a fatal error.
		MinRatio,
		// As bucket boundaries are iterated over from smallest to largest,
		// eliminating some due to either MinBound or MinRatio, if a bucket
		// boundary is reached that is larger than MaxBound, then the prior
		// kept bucket boundary becomes the last (largest) bucket boundary.
		MaxBound    float64
	}

	// If the number of buckets is large than MaxBuckets, then the metric is
	// just ignored and will not be exported to Prometheus.
	MaxBuckets  int

	// BadLabels specifies rules for identifying labels to be omitted from
	// the metrics exported to Prometheus.  This is usually used to remove
	// labels that would cause high-cardinality metrics.
	//
	// If a metric matches more than one rule, then any labels mentioned in
	// any of the matching rules will be omitted.
	BadLabels   []struct {
		Labels  []string
		Prefix  string
		Suffix  string
		Only    string
		Not     string
		Units   string
	}
}


type conf struct { config }

type ScalingFunc func(float64) float64


//// Global Variables ////

// The global configuration loaded from gcp2prom.yaml.
var Config conf

var Scale = map[string]ScalingFunc{
	"*1024*1024*1024":  multiply( 1024.0*1024.0*1024.0 ),
	"*1024*1024":       multiply( 1024.0*1024.0 ),
	"*60*60*24":        multiply( 60.0*60.0*24.0 ),
	"/100":             divide( 100.0 ),
	"/1000":            divide( 1000.0 ),
	"/1000/1000":       divide( 1000.0*1000.0 ),
	"/1000/1000/1000":  divide( 1000.0*1000.0*1000.0 ),
}


//// Functions ////


func multiply(m float64) ScalingFunc {
	return func(f float64) float64 { return f*m }
}

func divide(d float64) ScalingFunc {
	return func(f float64) float64 { return f/d }
}


func init() {
	r, err := os.Open("gcp2prom.yaml")
	if nil != err {
		lager.Exit().Map("Can't read gcp2prom.yaml", err)
	}
	y := yaml.NewDecoder(r)
	err = y.Decode(&Config.config)
	if nil != err {
		lager.Exit().Map("Invalid yaml in gcp2prom.yaml", err)
	}
	lager.Debug().Map("Loaded config", Config.config)
	h := Config.config.Histogram
	for k, v := range h {
		if strings.Contains(k, ",") {
			delete(h, k)
			for _, key := range strings.Split(k, ",") {
				h[key] = v
			}
		}
	}
	lager.Debug().Map("Histogram limits", Config.config.Histogram)
}


// Returns `nil` or a function that scales float64 values from the units
// used in StackDriver to the base units that are preferred in Prometheus.
func (c conf) Scaler(unit string) ScalingFunc {
	key := c.Units[unit]
	if "" == key {
		return nil
	}
	f, ok := Scale[key]
	if !ok {
		lager.Exit().Map("Unrecognized scale key", key, "For unit type", unit)
	}
	return f
}


// Returns the configured subsystem name for this metric type.
func (c conf) Subsystem(metricType string) string {
	parts := strings.Split(metricType, "/")
	e := len(parts)
	for 1 < e {
		e--
		prefix := strings.Join(parts[0:e], "/") + "/"
		if subsys := c.config.Subsystem[prefix]; "" != subsys {
			return subsys
		}
	}
	return ""
}


// Returns minBound, minRatio, and maxBound to use for histogram values when
// using the passed-in units.  If nothing is configured then 0.0, 0.0, 0.0 is
// returned.  Note that minBound and maxBound should be in the scaled (base)
// units used in Prometheus, not the StackDriver units given by the passed-in
// string.
func (c conf) HistogramLimits(unit string) (
	minBound, minRatio, maxBound float64,
) {
	h := c.Histogram[unit]
	return h.MinBound, h.MinRatio, h.MaxBound
}


// Returns the full metric name to use in Prometheus for the metric from
// StackDriver having the passed-in path, kind, and value type.
func (c conf) MetricName(path string, metricKind, valueType byte) string {
	for k, v := range c.Suffix.Any {
		if strings.HasSuffix(path, k) {
			path = path[0:len(path)-len(k)] + v
		}
	}
	var h map[string]string
	if 'H' == valueType {
		h = c.Suffix.Histogram
	} else if 'C' == metricKind || 'D' == metricKind {
		h = c.Suffix.Counter
	} else if 'G' == metricKind {
		h = c.Suffix.Gauge
	} else {
		lager.Panic().Map(
			"ValueType is not H", []byte{valueType},
			"MetricKind is no C nor G", []byte{metricKind},
		)
	}
	for k, v := range h {
		if strings.HasSuffix(path, k) {
			path = path[0:len(path)-len(k)] + v
		}
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}


// Returns `true` if either `k` or `t` is contained in the string `set`.
func Contains(set string, k, t byte) bool {
	any := string([]byte{k,t})
	return strings.ContainsAny(set, any)
}


// Returns the label names to be dropped when exporting the passed-in
// StackDriver metric to Prometheus.
func (c conf) BadLabels(md *monitoring.MetricDescriptor) []string {
	labels := make([]string, 0)
	k, t, u := mon.MetricAbbrs(md)
	path := md.Type
	for _, spec := range c.config.BadLabels {
		if "" != spec.Prefix && !strings.HasPrefix(path, spec.Prefix) ||
		   "" != spec.Suffix && !strings.HasSuffix(path, spec.Suffix) ||
		   "" != spec.Only && !Contains(spec.Only, k, t) ||
		   "" != spec.Not && Contains(spec.Not, k, t) ||
		   "" != spec.Units && u != spec.Units {
			continue
		}
		labels = append(labels, spec.Labels...)
	}
	return labels
}
