package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"osagentmvp/internal/models"
)

type Config struct {
	WorkspaceRoot         string
	DataDir               string
	ServerAddr            string
	BaseURL               string
	APIKey                string
	Model                 string
	RequestTimeoutSeconds int
	RunTimeoutSeconds     int
	KnownHostsPath        string
}

func Load(workspaceRoot string) (Config, error) {
	if err := loadDotEnv(filepath.Join(workspaceRoot, ".env")); err != nil {
		return Config{}, err
	}

	cfg := Config{
		WorkspaceRoot:         workspaceRoot,
		DataDir:               envOrDefault("OSAGENT_DATA_DIR", "data"),
		ServerAddr:            envOrDefault("OSAGENT_SERVER_ADDR", ":7778"),
		BaseURL:               envOrDefault("OSAGENT_LLM_BASE_URL", "https://api.longcat.chat"),
		APIKey:                strings.TrimSpace(os.Getenv("OSAGENT_LLM_API_KEY")),
		Model:                 envOrDefault("OSAGENT_LLM_MODEL", "LongCat-Flash-Thinking-2601"),
		RequestTimeoutSeconds: envIntOrDefault("OSAGENT_REQUEST_TIMEOUT_SECONDS", 120),
		RunTimeoutSeconds:     envIntOrDefault("OSAGENT_RUN_TIMEOUT_SECONDS", 180),
		KnownHostsPath:        strings.TrimSpace(os.Getenv("OSAGENT_KNOWN_HOSTS")),
	}

	if cfg.RequestTimeoutSeconds <= 0 {
		return Config{}, errors.New("OSAGENT_REQUEST_TIMEOUT_SECONDS must be positive")
	}
	if cfg.RunTimeoutSeconds <= 0 {
		return Config{}, errors.New("OSAGENT_RUN_TIMEOUT_SECONDS must be positive")
	}

	if err := os.MkdirAll(cfg.AbsDataDir(), 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	return cfg, nil
}

func (c Config) DefaultGatewayConfig() models.GatewayConfig {
	now := time.Now().UTC()
	return models.GatewayConfig{
		CurrentPresetID: "default",
		UpdatedAt:       now,
		Presets: []models.GatewayPreset{
			{
				ID:        "default",
				Name:      "默认预设",
				BaseURL:   strings.TrimSpace(c.BaseURL),
				APIKey:    strings.TrimSpace(c.APIKey),
				Model:     strings.TrimSpace(c.Model),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
}

func (c Config) AbsDataDir() string {
	if filepath.IsAbs(c.DataDir) {
		return c.DataDir
	}
	return filepath.Join(c.WorkspaceRoot, c.DataDir)
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open .env: %w", err)
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
			return fmt.Errorf("invalid .env line: %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return scanner.Err()
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envIntOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
