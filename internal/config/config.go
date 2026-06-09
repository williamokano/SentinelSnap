package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPPort               int
	Debug                  bool
	HTTPSEnabled           bool
	ShutdownTimeoutSeconds int

	DBDriver string
	DBDSN    string

	StorageBackend string

	LocalUploadDir string
	LocalBaseURL   string

	S3Bucket    string
	S3Region    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string

	// Logging (always active)
	LogLevel  string
	LogFormat string

	// OTel exporter settings — endpoint, protocol, sampling, and per-signal modes.
	OtelEnabled     bool
	OtelServiceName string
	OTLPEndpoint    string
	OTLPProtocol    string
	MetricsMode     string
	TracesMode      string
	LogsMode        string
	TraceSampleRate float64
}

func Load() (*Config, error) {
	// Load env file; ENV_FILE overrides the default ".env"
	_ = godotenv.Load(getEnv("ENV_FILE", ".env"))

	port := getEnvInt("HTTP_PORT", 8080)
	shutdownTimeout := getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", 30)
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("required env var DB_DSN is not set")
	}
	httpsEnabled, _ := strconv.ParseBool(os.Getenv("HTTPS_ENABLED"))
	return &Config{
		HTTPPort:               port,
		Debug:                  os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1",
		HTTPSEnabled:           httpsEnabled,
		ShutdownTimeoutSeconds: shutdownTimeout,
		DBDriver:               getEnv("DB_DRIVER", "postgres"),
		DBDSN:                  dsn,
		StorageBackend:         getEnv("STORAGE_BACKEND", "local"),
		LocalUploadDir:         getEnv("LOCAL_UPLOAD_DIR", "./uploads"),
		LocalBaseURL:           getEnv("LOCAL_BASE_URL", "http://localhost:8080/uploads"),
		S3Bucket:               os.Getenv("S3_BUCKET"),
		S3Region:               os.Getenv("S3_REGION"),
		S3Endpoint:             os.Getenv("S3_ENDPOINT"),
		S3AccessKey:            os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:            os.Getenv("S3_SECRET_KEY"),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		LogFormat:              getEnv("LOG_FORMAT", "json"),
		OtelEnabled:            getEnvBool("OTEL_ENABLED"),
		OtelServiceName:        getEnv("OTEL_SERVICE_NAME", "sentinelsnap"),
		OTLPEndpoint:           getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317"),
		OTLPProtocol:           getEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc"),
		MetricsMode:            getEnv("OTEL_METRICS_MODE", "pull"),
		TracesMode:             getEnv("OTEL_TRACES_MODE", "push"),
		LogsMode:               getEnv("OTEL_LOGS_MODE", "stdout"),
		TraceSampleRate:        getEnvFloat("OTEL_TRACES_SAMPLER_ARG", 1.0),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return fallback
}

func getEnvBool(key string) bool {
	v := os.Getenv(key)
	return v == "true" || v == "1"
}
