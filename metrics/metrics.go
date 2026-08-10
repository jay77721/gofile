package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 指标定义（client_golang 惯例：包级变量 + 默认注册表）
var (
	// httpRequestsTotal HTTP 请求计数器，按 method/path/status 分桶
	// path 使用 gin 路由模板（如 /file/download），不含 query 参数，避免标签基数爆炸
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed, partitioned by method, path and status code.",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration HTTP 请求耗时直方图，按 method/path 分桶
	// 不含 status 标签，进一步控制基数；status 差异已由 http_requests_total 覆盖
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds, partitioned by method and path.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// fileUploadBytes 业务指标：累计上传成功的文件字节数
	// 普通 Counter（不按 username 分桶）——用户名是高基数标签，会撑爆 Prometheus 内存
	fileUploadBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "file_upload_bytes_total",
			Help: "Total bytes of files successfully uploaded (includes chunked merges).",
		},
	)

	// aiTasksTotal AI 异步任务计数器，按 status 分桶（pending/done/failed）
	aiTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_tasks_total",
			Help: "Total number of AI analysis tasks processed, partitioned by status.",
		},
		[]string{"status"},
	)

	// aiLLMDuration LLM 调用耗时直方图（Analyze + Embed）
	aiLLMDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ai_llm_duration_seconds",
			Help:    "Duration of LLM API calls in seconds, partitioned by operation.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// aiIndexOps 检索引擎操作计数器，按 op/result 分桶
	aiIndexOps = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_index_ops_total",
			Help: "Total number of AI index operations, partitioned by operation and result.",
		},
		[]string{"operation", "result"},
	)

	registerOnce sync.Once
)

// Register 将指标注册到默认注册表。幂等，可安全重复调用（测试 + main 多次调用）。
func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, fileUploadBytes, aiTasksTotal, aiLLMDuration, aiIndexOps)
	})
}

// RecordHTTPRequest 由 MetricsMiddleware 在请求结束时调用
func RecordHTTPRequest(method, path, status string, durSec float64) {
	httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	httpRequestDuration.WithLabelValues(method, path).Observe(durSec)
}

// AddUploadBytes 由 service 层在上传成功后调用，累计业务上传字节
func AddUploadBytes(bytes int64) {
	fileUploadBytes.Add(float64(bytes))
}

// RecordAITask 由 ai.Processor 在任务状态变更时调用
func RecordAITask(status string) {
	aiTasksTotal.WithLabelValues(status).Inc()
}

// ObserveLLMDuration 由 ai.Provider 调用方记录 LLM 耗时
func ObserveLLMDuration(operation string, durSec float64) {
	aiLLMDuration.WithLabelValues(operation).Observe(durSec)
}

// RecordIndexOp 由 ai.Indexer 调用方记录检索引擎操作
func RecordIndexOp(operation, result string) {
	aiIndexOps.WithLabelValues(operation, result).Inc()
}

// Handler 返回 /metrics 抓取端点
// 使用默认注册表，附带 go/process 运行时指标（go_goroutines 等）
func Handler() http.Handler {
	Register()
	return promhttp.Handler()
}
