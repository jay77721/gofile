package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metric definitions (client_golang convention: package-level variables + default registry)
var (
	// httpRequestsTotal HTTP request counter, partitioned by method/path/status
	// path uses the Gin route template (e.g., /file/download), without query parameters to avoid label cardinality explosion
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed, partitioned by method, path and status code.",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration HTTP request duration histogram, partitioned by method/path
	// Does not include status label to further control cardinality; status differences are already covered by http_requests_total
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds, partitioned by method and path.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// fileUploadBytes is a business metric: total bytes of files successfully uploaded
	// Plain Counter (not partitioned by username) — username is a high-cardinality label that would exhaust Prometheus memory
	fileUploadBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "file_upload_bytes_total",
			Help: "Total bytes of files successfully uploaded (includes chunked merges).",
		},
	)

	// aiTasksTotal AI async task counter, partitioned by status (pending/done/failed)
	aiTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_tasks_total",
			Help: "Total number of AI analysis tasks processed, partitioned by status.",
		},
		[]string{"status"},
	)

	// aiLLMDuration LLM call duration histogram (Analyze + Embed)
	aiLLMDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ai_llm_duration_seconds",
			Help:    "Duration of LLM API calls in seconds, partitioned by operation.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// aiIndexOps Search engine operation counter, partitioned by op/result
	aiIndexOps = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_index_ops_total",
			Help: "Total number of AI index operations, partitioned by operation and result.",
		},
		[]string{"operation", "result"},
	)

	registerOnce sync.Once
)

// Register Register metrics to the default registry. Idempotent and safe to call repeatedly (tests + main).
func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, fileUploadBytes, aiTasksTotal, aiLLMDuration, aiIndexOps)
	})
}

// RecordHTTPRequest called by MetricsMiddleware at the end of a request
func RecordHTTPRequest(method, path, status string, durSec float64) {
	httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	httpRequestDuration.WithLabelValues(method, path).Observe(durSec)
}

// AddUploadBytes called by the service layer after a successful upload to accumulate business upload bytes
func AddUploadBytes(bytes int64) {
	fileUploadBytes.Add(float64(bytes))
}

// RecordAITask called by ai.Processor when task status changes
func RecordAITask(status string) {
	aiTasksTotal.WithLabelValues(status).Inc()
}

// ObserveLLMDuration called by port.Provider callers to record LLM duration
func ObserveLLMDuration(operation string, durSec float64) {
	aiLLMDuration.WithLabelValues(operation).Observe(durSec)
}

// RecordIndexOp called by port.Indexer callers to record search engine operations
func RecordIndexOp(operation, result string) {
	aiIndexOps.WithLabelValues(operation, result).Inc()
}

// Handler return the /metrics scrape endpoint
// Use the default registry with go/process runtime metrics (go_goroutines, etc.)
func Handler() http.Handler {
	Register()
	return promhttp.Handler()
}
