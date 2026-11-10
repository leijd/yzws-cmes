package config

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"clothp/internal/sms"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode                           string
	Listen                         string
	ReadHeaderTimeout              time.Duration
	NodeID                         string
	JWTSecret                      string
	BootstrapAdminPassword         string
	BootstrapPlatformAdminPassword string
	AttachmentDir                  string
	// PublicBaseURL 用于生成二维码中的扫码地址（例如 http://192.168.1.10:18379）。
	// 为空时按请求的 Origin/Referer/Host 推导，适合局域网直接访问 Web 端。
	PublicBaseURL     string
	AccessTokenTTL    time.Duration
	OfflineSessionTTL time.Duration
	Database          Database
	SMS               sms.Settings
}

type Database struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
	MaxConns int
}

type fileConfig struct {
	Mode          string `yaml:"mode"`
	PublicBaseURL string `yaml:"public_base_url"`
	Server        struct {
		Listen              string `yaml:"listen"`
		ReadHeaderTimeoutMS int    `yaml:"read_header_timeout_ms"`
	} `yaml:"server"`
	Node struct {
		ID string `yaml:"id"`
	} `yaml:"node"`
	Storage struct {
		AttachmentDir string `yaml:"attachment_dir"`
	} `yaml:"storage"`
	Security struct {
		AccessTokenMinutes  int `yaml:"access_token_minutes"`
		OfflineSessionHours int `yaml:"offline_session_hours"`
	} `yaml:"security"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Name     string `yaml:"name"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		SSLMode  string `yaml:"sslmode"`
		MaxConns int    `yaml:"max_conns"`
	} `yaml:"database"`
	SMS sms.Settings `yaml:"sms"`
}

func defaults() Config {
	return Config{
		Mode: "center", Listen: ":18370", ReadHeaderTimeout: 10 * time.Second,
		NodeID: "center-local", JWTSecret: "local-development-only-change-me",
		BootstrapAdminPassword: "ChangeMe123!", BootstrapPlatformAdminPassword: "PlatformAdmin123!", AttachmentDir: "data/attachments",
		AccessTokenTTL: 2 * time.Hour, OfflineSessionTTL: 7 * 24 * time.Hour,
		Database: Database{Host: "localhost", Port: 5432, Name: "clothpm", User: "postgres", SSLMode: "disable", MaxConns: 20},
		SMS:      sms.Settings{CooldownSeconds: 60, FailureThreshold: 3, UsageFile: "data/sms_usage.json"},
	}
}

// Load 加载配置。explicitPath 为启动时 -config 指定的配置文件路径（也可通过
// CLOTHP_CONFIG 环境变量指定）。指定了配置文件时该文件必须存在，且文件中的
// 配置项优先于环境变量（环境变量仅填充文件未配置的项）；未指定时读取默认的
// config.yaml（可选），环境变量优先。
func Load(explicitPath string) (Config, error) {
	cfg := defaults()
	path := explicitPath
	if path == "" {
		path = os.Getenv("CLOTHP_CONFIG")
	}
	if path != "" {
		fromFile, err := readFileConfig(path)
		if err != nil {
			return Config{}, err
		}
		applyEnv(&cfg)
		cfg.Database.Password = os.Getenv("CLOTHP_DB_PASSWORD")
		applyFile(&cfg, fromFile)
		log.Printf("config: using specified file %s (file values take precedence over environment)", path)
	} else {
		body, err := os.ReadFile("config.yaml")
		if err == nil {
			var fromFile fileConfig
			decoder := yaml.NewDecoder(bytes.NewReader(body))
			decoder.KnownFields(true)
			if err := decoder.Decode(&fromFile); err != nil {
				return Config{}, fmt.Errorf("parse config %s: %w", "config.yaml", err)
			}
			applyFile(&cfg, fromFile)
			log.Printf("config: using default file config.yaml (environment takes precedence)")
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("read config %s: %w", "config.yaml", err)
		}
		applyEnv(&cfg)
		if envPassword := os.Getenv("CLOTHP_DB_PASSWORD"); envPassword != "" {
			cfg.Database.Password = envPassword
		}
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readFileConfig(path string) (fileConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var fromFile fileConfig
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fromFile); err != nil {
		return fileConfig{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return fromFile, nil
}

func applyFile(cfg *Config, file fileConfig) {
	if file.Mode != "" {
		cfg.Mode = file.Mode
	}
	if file.PublicBaseURL != "" {
		cfg.PublicBaseURL = file.PublicBaseURL
	}
	if file.Server.Listen != "" {
		cfg.Listen = file.Server.Listen
	}
	if file.Server.ReadHeaderTimeoutMS > 0 {
		cfg.ReadHeaderTimeout = time.Duration(file.Server.ReadHeaderTimeoutMS) * time.Millisecond
	}
	if file.Node.ID != "" {
		cfg.NodeID = file.Node.ID
	}
	if file.Storage.AttachmentDir != "" {
		cfg.AttachmentDir = file.Storage.AttachmentDir
	}
	if file.Security.AccessTokenMinutes > 0 {
		cfg.AccessTokenTTL = time.Duration(file.Security.AccessTokenMinutes) * time.Minute
	}
	if file.Security.OfflineSessionHours > 0 {
		cfg.OfflineSessionTTL = time.Duration(file.Security.OfflineSessionHours) * time.Hour
	}
	if file.Database.Host != "" {
		cfg.Database.Host = file.Database.Host
	}
	if file.Database.Port > 0 {
		cfg.Database.Port = file.Database.Port
	}
	if file.Database.Name != "" {
		cfg.Database.Name = file.Database.Name
	}
	if file.Database.User != "" {
		cfg.Database.User = file.Database.User
	}
	if file.Database.Password != "" {
		cfg.Database.Password = file.Database.Password
	}
	if file.Database.SSLMode != "" {
		cfg.Database.SSLMode = file.Database.SSLMode
	}
	if file.Database.MaxConns > 0 {
		cfg.Database.MaxConns = file.Database.MaxConns
	}
	/* 短信服务商：文件里配置了才覆盖默认值，未配置时保持演示模式 */
	if len(file.SMS.Providers) > 0 {
		cfg.SMS.Providers = file.SMS.Providers
	}
	if file.SMS.CooldownSeconds > 0 {
		cfg.SMS.CooldownSeconds = file.SMS.CooldownSeconds
	}
	if file.SMS.FailureThreshold > 0 {
		cfg.SMS.FailureThreshold = file.SMS.FailureThreshold
	}
	if file.SMS.UsageFile != "" {
		cfg.SMS.UsageFile = file.SMS.UsageFile
	}
}

func applyEnv(cfg *Config) {
	cfg.Mode = env("CLOTHP_MODE", cfg.Mode)
	cfg.Listen = env("CLOTHP_LISTEN", cfg.Listen)
	cfg.NodeID = env("CLOTHP_NODE_ID", cfg.NodeID)
	cfg.AttachmentDir = env("CLOTHP_ATTACHMENT_DIR", cfg.AttachmentDir)
	cfg.PublicBaseURL = env("CLOTHP_PUBLIC_BASE_URL", cfg.PublicBaseURL)
	cfg.JWTSecret = env("CLOTHP_JWT_SECRET", cfg.JWTSecret)
	cfg.BootstrapAdminPassword = env("CLOTHP_ADMIN_PASSWORD", cfg.BootstrapAdminPassword)
	cfg.BootstrapPlatformAdminPassword = env("CLOTHP_PLATFORM_ADMIN_PASSWORD", cfg.BootstrapPlatformAdminPassword)
	cfg.ReadHeaderTimeout = time.Duration(envInt("CLOTHP_READ_HEADER_TIMEOUT_MS", int(cfg.ReadHeaderTimeout.Milliseconds()))) * time.Millisecond
	cfg.AccessTokenTTL = time.Duration(envInt("CLOTHP_ACCESS_TOKEN_MINUTES", int(cfg.AccessTokenTTL.Minutes()))) * time.Minute
	cfg.OfflineSessionTTL = time.Duration(envInt("CLOTHP_OFFLINE_SESSION_HOURS", int(cfg.OfflineSessionTTL.Hours()))) * time.Hour
	cfg.Database.Host = env("DB_HOST", cfg.Database.Host)
	cfg.Database.Port = envInt("DB_PORT", cfg.Database.Port)
	cfg.Database.Name = env("DB_NAME", cfg.Database.Name)
	cfg.Database.User = env("DB_USER", cfg.Database.User)
	cfg.Database.SSLMode = env("DB_SSLMODE", cfg.Database.SSLMode)
	cfg.Database.MaxConns = envInt("DB_MAX_CONNS", cfg.Database.MaxConns)
}

func validate(cfg Config) error {
	if cfg.Mode == "" || cfg.Listen == "" || cfg.NodeID == "" || cfg.AttachmentDir == "" {
		return fmt.Errorf("config mode, listen, node.id and storage.attachment_dir are required")
	}
	if cfg.Database.Host == "" || cfg.Database.Name == "" || cfg.Database.User == "" || cfg.Database.SSLMode == "" {
		return fmt.Errorf("config database host, name, user and sslmode are required")
	}
	if cfg.Database.Port < 1 || cfg.Database.Port > 65535 || cfg.Database.MaxConns < 1 {
		return fmt.Errorf("config database port or max_conns is invalid")
	}
	if cfg.ReadHeaderTimeout <= 0 || cfg.AccessTokenTTL <= 0 || cfg.OfflineSessionTTL <= 0 {
		return fmt.Errorf("config timeouts and session durations must be positive")
	}
	if cfg.JWTSecret == "" || cfg.BootstrapAdminPassword == "" || cfg.BootstrapPlatformAdminPassword == "" {
		return fmt.Errorf("CLOTHP_JWT_SECRET, CLOTHP_ADMIN_PASSWORD and CLOTHP_PLATFORM_ADMIN_PASSWORD cannot be empty")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value == 0 {
		return fallback
	}
	return value
}
