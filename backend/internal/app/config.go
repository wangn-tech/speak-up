package app

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Address       string
	MySQLDSN      string
	RedisAddress  string
	KafkaBrokers  []string
	AIAddress     string
	PublicWSURL   string
	JWTSecret     string
	AllowedOrigin string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	WSTTL         time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		Address:       env("GATEWAY_ADDR", ":8080"),
		MySQLDSN:      env("MYSQL_DSN", "speakup:speakup@tcp(127.0.0.1:13306)/speakup?charset=utf8mb4&parseTime=True&multiStatements=true"),
		RedisAddress:  env("REDIS_ADDR", "127.0.0.1:16379"),
		KafkaBrokers:  splitCSV(env("KAFKA_BROKERS", "127.0.0.1:19092")),
		AIAddress:     env("AI_GRPC_ADDR", "127.0.0.1:50051"),
		PublicWSURL:   env("PUBLIC_WS_URL", "ws://127.0.0.1:8080/ws/conversation"),
		JWTSecret:     env("JWT_SECRET", "local-development-secret-change-me"),
		AllowedOrigin: env("FRONTEND_ORIGIN", "http://localhost:5173"),
		AccessTTL:     2 * time.Hour,
		RefreshTTL:    7 * 24 * time.Hour,
		WSTTL:         2 * time.Minute,
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}
