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
