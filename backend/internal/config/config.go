package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv        string
	PublicURL     string
	HTTPAddr      string
	PostgresDSN   string
	UploadRoot    string
	JWTIssuer     string
	JWTAudience   string
	JWTSecret     string
	CookieSecure  bool
	LogLevel      string
	LiveKitURL    string
	LiveKitKey    string
	LiveKitSecret string
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
	IdleTimeout   time.Duration
}

func Load() (Config, error) {
	_ = loadDotEnv(".env")
	_ = loadDotEnv("backend/.env")

	cfg := Config{
		AppEnv:        getEnv("APP_ENV", "development"),
		PublicURL:     getEnv("APP_PUBLIC_URL", "http://localhost:5173"),
		HTTPAddr:      getEnv("HTTP_ADDR", "127.0.0.1:8080"),
		PostgresDSN:   getEnv("POSTGRES_DSN", ""),
		UploadRoot:    getEnv("UPLOAD_ROOT", "../uploads"),
		JWTIssuer:     getEnv("JWT_ISSUER", "echorift"),
		JWTAudience:   getEnv("JWT_AUDIENCE", "echorift-api"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		CookieSecure:  getBoolEnv("COOKIE_SECURE", true),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		LiveKitURL:    getEnv("LIVEKIT_URL", ""),
		LiveKitKey:    getEnv("LIVEKIT_API_KEY", ""),
		LiveKitSecret: getEnv("LIVEKIT_API_SECRET", ""),
		ReadTimeout:   10 * time.Second,
		WriteTimeout:  20 * time.Second,
		IdleTimeout:   75 * time.Second,
	}

	if cfg.PostgresDSN == "" {
		return Config{}, errors.New("POSTGRES_DSN is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
