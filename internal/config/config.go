package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	ServerAddr string // server listen address, default :8080
	MySQLDSN   string // MySQL DSN, required
	UploadDir  string // file upload directory, default ./uploads
	ChunkDir   string // chunk upload directory, default ./chunks

	MinioEndpoint  string // MinIO endpoint, default minio:9000
	MinioAccessKey string // MinIO AccessKey, default minioadmin
	MinioSecretKey string // MinIO SecretKey, default minioadmin
	MinioBucket    string // MinIO Bucket, default filestore
	MinioUseSSL    bool   // whether to use SSL, default false

	CookieSecure bool // true in production, Cookie is only sent over HTTPS

	RedisAddr     string // Redis address, default localhost:6379
	RedisPassword string // Redis password, default empty
	RedisDB       int    // Redis DB number, default 0

	// AI feature configuration
	AIEnabled         bool   // AI feature master switch, default false
	AIProvider        string // LLM provider: mock | anthropic | openai, default mock
	AIAPIKey          string // LLM API Key (ignored under mock)
	AIModel           string // LLM model name (empty uses each provider's default)
	AIEmbedDim        int    // vector dimension, default 128
	AIWorkers         int    // async worker count, default 4
	TypesenseURL      string // Typesense URL, default http://localhost:8108
	TypesenseAPIKey   string // Typesense API Key, default xyz
	AIConfigSecret    string // AES key for user custom API key encryption, derived from DSN when missing
	AllowPrivateAIURL bool   // whether to allow custom baseURL to point to private network (default false, enable for local Ollama)

	// AsynqEnabled whether to enable Asynq persistent task queue (replaces in-process chan, requires Redis)
	// true: tasks are written to Redis, shared across instances, with built-in retry + dead-letter queue
	// false (default): fallback to in-process chan (lost on restart, single instance)
	AsynqEnabled bool
}

// Load loads configuration from environment variables with sensible defaults.
func Load() *Config {
	loadDotEnv()

	cfg := &Config{
		ServerAddr: getEnv("SERVER_ADDR", ":8080"),
		MySQLDSN:   getEnv("MYSQL_DSN", ""),
		UploadDir:  getEnv("UPLOAD_DIR", "./uploads"),
		ChunkDir:   getEnv("CHUNK_DIR", "./chunks"),

		MinioEndpoint:  getEnv("MINIO_ENDPOINT", "minio:9000"),
		MinioAccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		MinioSecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
		MinioBucket:    getEnv("MINIO_BUCKET", "filestore"),
		MinioUseSSL:    getEnvBool("MINIO_USE_SSL", false),

		CookieSecure: getEnvBool("COOKIE_SECURE", false),

		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		AIEnabled:         getEnvBool("AI_ENABLED", false),
		AIProvider:        getEnv("AI_PROVIDER", "mock"),
		AIAPIKey:          getEnv("AI_API_KEY", ""),
		AIModel:           getEnv("AI_MODEL", ""),
		AIEmbedDim:        getEnvInt("AI_EMBED_DIM", 128),
		AIWorkers:         getEnvInt("AI_WORKERS", 4),
		TypesenseURL:      getEnv("TYPESENSE_URL", "http://localhost:8108"),
		TypesenseAPIKey:   getEnv("TYPESENSE_API_KEY", "xyz"),
		AIConfigSecret:    getEnv("AI_CONFIG_SECRET", ""),
		AllowPrivateAIURL: getEnvBool("ALLOW_PRIVATE_AI_URL", false),

		AsynqEnabled: getEnvBool("ASYNQ_ENABLED", false),
	}

	// use default when MySQL DSN is not set (local dev)
	if cfg.MySQLDSN == "" {
		cfg.MySQLDSN = "root:root@tcp(127.0.0.1:3306)/gofile?charset=utf8mb4&parseTime=True&loc=Local"
	}

	return cfg
}

// AIConfigSecretKey returns the API key encryption secret:
// prefers AI_CONFIG_SECRET; when not configured, derives from MySQL DSN (ensures existing ciphertext remains decryptable after restart).
func (c *Config) AIConfigSecretKey() string {
	if c.AIConfigSecret != "" {
		return c.AIConfigSecret
	}
	return "gofile-secret:" + c.MySQLDSN
}

// loadDotEnv loads variables from .env file (does not override existing env vars).
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if val == "true" || val == "1" {
			return true
		}
		return false
	}
	return defaultVal
}
