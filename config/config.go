package config

import (
	"os"
	"strconv"
)

// Config 应用配置，从环境变量读取
type Config struct {
	ServerAddr string // 服务监听地址，默认 :8080
	MySQLDSN   string // MySQL 连接字符串，必填
	RedisAddr  string // Redis 地址，默认 127.0.0.1:6379
	RedisPass  string // Redis 密码，默认空
	RedisDB    int    // Redis DB 编号，默认 0
	UploadDir  string // 文件上传目录，默认 ./uploads
	ChunkDir   string // 分块上传目录，默认 ./chunks
}

// Load 从环境变量加载配置，提供合理默认值
func Load() *Config {
	cfg := &Config{
		ServerAddr: getEnv("SERVER_ADDR", ":8080"),
		MySQLDSN:   getEnv("MYSQL_DSN", ""),
		RedisAddr:  getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPass:  getEnv("REDIS_PASS", ""),
		RedisDB:    getEnvInt("REDIS_DB", 0),
		UploadDir:  getEnv("UPLOAD_DIR", "./uploads"),
		ChunkDir:   getEnv("CHUNK_DIR", "./chunks"),
	}

	// MySQL DSN 未设置时使用默认值（本地开发）
	if cfg.MySQLDSN == "" {
		cfg.MySQLDSN = "root:root@tcp(127.0.0.1:3306)/fileserver?charset=utf8mb4&parseTime=True&loc=Local"
	}

	return cfg
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
