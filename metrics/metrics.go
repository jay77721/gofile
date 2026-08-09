package metrics

import (
	"net/http"

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

	registered bool // 幂等注册守卫，防止重复 MustRegister panic
)

// Register 将指标注册到默认注册表。幂等，可安全重复调用（测试 + main 多次调用）。
func Register() {
	if registered {
		return
	}
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, fileUploadBytes)
	registered = true
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

// Handler 返回 /metrics 抓取端点
// 使用默认注册表，附带 go/process 运行时指标（go_goroutines 等）
func Handler() http.Handler {
	Register()
	return promhttp.Handler()
}
