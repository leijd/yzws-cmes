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
	/* SecureDatabase 非空时启用双库（plain=Database，secure=SecureDatabase）；
	 * 为空时保持单库行为，write_mode 强制 plain_only。 */
	SecureDatabase *Database
	DataStore      DataStoreConfig
	Crypto         CryptoConfig
	SMS            sms.Settings
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

/* DataStoreConfig 双库存储口径（设计 3.1/3.2）：默认 write_mode=both、read_source=secure。 */
type DataStoreConfig struct {
	ReadSource               string // secure（默认）| plain
	WriteMode                string // both（默认）| plain_only | secure_only
	AllowDegradedRead        bool
	ReconcileIntervalSeconds int
}

/* CryptoConfig 信封加密与密钥提供方（设计 3.2/3.4）。 */
type CryptoConfig struct {
	EnvelopeVersion string // 如 v1
	DataAlgorithm   string // AES-256-GCM
	WrapAlgorithm   string // RSA-3072-OAEP-SHA256
	KMSProvider     string // softhsm2（测试）| cloud_kms（预留）| local（仅迁移旧数据）
	SoftHSM2        SoftHSM2Config
	CloudKMS        CloudKMSConfig
}

type SoftHSM2Config struct {
	PKCS11ModuleEnv string // 环境变量名，模块路径只从环境注入
	TokenLabel      string
	PINEnv          string // 环境变量名，PIN 只从环境注入
	KeyLabelPrefix  string
}

type CloudKMSConfig struct {
	Provider    string
	Region      string
	KeyIDPrefix string
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
	Databases struct {
		Plain  fileDatabase `yaml:"plain"`
		Secure fileDatabase `yaml:"secure"`
	} `yaml:"databases"`
	DataStore struct {
		ReadSource               string `yaml:"read_source"`
		WriteMode                string `yaml:"write_mode"`
		AllowDegradedRead        bool   `yaml:"allow_degraded_read"`
		ReconcileIntervalSeconds int    `yaml:"reconcile_interval_seconds"`
	} `yaml:"data_store"`
	Crypto struct {
		EnvelopeVersion string `yaml:"envelope_version"`
		DataAlgorithm   string `yaml:"data_algorithm"`
		WrapAlgorithm   string `yaml:"wrap_algorithm"`
		KMSProvider     string `yaml:"kms_provider"`
		SoftHSM2        struct {
			PKCS11ModuleEnv string `yaml:"pkcs11_module_env"`
			TokenLabel      string `yaml:"token_label"`
			PINEnv          string `yaml:"pin_env"`
			KeyLabelPrefix  string `yaml:"key_label_prefix"`
		} `yaml:"softhsm2"`
		CloudKMS struct {
			Provider    string `yaml:"provider"`
			Region      string `yaml:"region"`
			KeyIDPrefix string `yaml:"key_id_prefix"`
		} `yaml:"cloud_kms"`
	} `yaml:"crypto"`
	SMS sms.Settings `yaml:"sms"`
}

type fileDatabase struct {
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Name        string `yaml:"name"`
	User        string `yaml:"user"`
	Password    string `yaml:"password"`
	PasswordEnv string `yaml:"password_env"`
	SSLMode     string `yaml:"sslmode"`
	MaxConns    int    `yaml:"max_conns"`
}

func defaults() Config {
	return Config{
		Mode: "center", Listen: ":18370", ReadHeaderTimeout: 10 * time.Second,
		NodeID: "center-local", JWTSecret: "local-development-only-change-me",
		BootstrapAdminPassword: "ChangeMe123!", BootstrapPlatformAdminPassword: "PlatformAdmin123!", AttachmentDir: "data/attachments",
		AccessTokenTTL: 2 * time.Hour, OfflineSessionTTL: 7 * 24 * time.Hour,
		Database:  Database{Host: "localhost", Port: 5432, Name: "clothpm", User: "postgres", SSLMode: "disable", MaxConns: 20},
		DataStore: DataStoreConfig{ReadSource: "secure", WriteMode: "both", ReconcileIntervalSeconds: 60},
		Crypto: CryptoConfig{
			EnvelopeVersion: "v1", DataAlgorithm: "AES-256-GCM", WrapAlgorithm: "RSA-3072-OAEP-SHA256",
			KMSProvider: "local", // 未配置 SoftHSM2/云 KMS 时仅用于迁移旧数据，不得用于新密钥
			SoftHSM2:  SoftHSM2Config{TokenLabel: "yzws-mes-test", KeyLabelPrefix: "yzws-tenant-"},
			CloudKMS:  CloudKMSConfig{},
		},
		SMS: sms.Settings{CooldownSeconds: 60, FailureThreshold: 3, UsageFile: "data/sms_usage.json"},
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
		if cfg.SecureDatabase != nil && cfg.SecureDatabase.Password == "" {
			cfg.SecureDatabase.Password = os.Getenv("CLOTHP_DB_SECURE_PASSWORD")
		}
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
		if cfg.SecureDatabase != nil && cfg.SecureDatabase.Password == "" {
			cfg.SecureDatabase.Password = os.Getenv("CLOTHP_DB_SECURE_PASSWORD")
		}
	}
	/* 未配置 secure 库时保持单库行为：只写明文库、从明文库读取 */
	if cfg.SecureDatabase == nil {
		cfg.DataStore.WriteMode = "plain_only"
		cfg.DataStore.ReadSource = "plain"
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
	/* 双库：databases.plain 覆盖明文库；databases.secure 存在时启用双库 */
	if file.Databases.Plain.Host != "" {
		applyDatabase(&cfg.Database, file.Databases.Plain)
	}
	if file.Databases.Secure.Host != "" {
		secure := cfg.Database
		applyDatabase(&secure, file.Databases.Secure)
		cfg.SecureDatabase = &secure
	}
	if file.DataStore.ReadSource != "" {
		cfg.DataStore.ReadSource = file.DataStore.ReadSource
	}
	if file.DataStore.WriteMode != "" {
		cfg.DataStore.WriteMode = file.DataStore.WriteMode
	}
	cfg.DataStore.AllowDegradedRead = file.DataStore.AllowDegradedRead
	if file.DataStore.ReconcileIntervalSeconds > 0 {
		cfg.DataStore.ReconcileIntervalSeconds = file.DataStore.ReconcileIntervalSeconds
	}
	if file.Crypto.EnvelopeVersion != "" {
		cfg.Crypto.EnvelopeVersion = file.Crypto.EnvelopeVersion
	}
	if file.Crypto.DataAlgorithm != "" {
		cfg.Crypto.DataAlgorithm = file.Crypto.DataAlgorithm
	}
	if file.Crypto.WrapAlgorithm != "" {
		cfg.Crypto.WrapAlgorithm = file.Crypto.WrapAlgorithm
	}
	if file.Crypto.KMSProvider != "" {
		cfg.Crypto.KMSProvider = file.Crypto.KMSProvider
	}
	if file.Crypto.SoftHSM2.PKCS11ModuleEnv != "" {
		cfg.Crypto.SoftHSM2.PKCS11ModuleEnv = file.Crypto.SoftHSM2.PKCS11ModuleEnv
	}
	if file.Crypto.SoftHSM2.TokenLabel != "" {
		cfg.Crypto.SoftHSM2.TokenLabel = file.Crypto.SoftHSM2.TokenLabel
	}
	if file.Crypto.SoftHSM2.PINEnv != "" {
		cfg.Crypto.SoftHSM2.PINEnv = file.Crypto.SoftHSM2.PINEnv
	}
	if file.Crypto.SoftHSM2.KeyLabelPrefix != "" {
		cfg.Crypto.SoftHSM2.KeyLabelPrefix = file.Crypto.SoftHSM2.KeyLabelPrefix
	}
	if file.Crypto.CloudKMS.Provider != "" {
		cfg.Crypto.CloudKMS.Provider = file.Crypto.CloudKMS.Provider
	}
	if file.Crypto.CloudKMS.Region != "" {
		cfg.Crypto.CloudKMS.Region = file.Crypto.CloudKMS.Region
	}
	if file.Crypto.CloudKMS.KeyIDPrefix != "" {
		cfg.Crypto.CloudKMS.KeyIDPrefix = file.Crypto.CloudKMS.KeyIDPrefix
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

/* applyDatabase 用文件中的数据库段覆盖目标配置；password_env 指定的环境变量优先。 */
func applyDatabase(target *Database, in fileDatabase) {
	if in.Host != "" {
		target.Host = in.Host
	}
	if in.Port > 0 {
		target.Port = in.Port
	}
	if in.Name != "" {
		target.Name = in.Name
	}
	if in.User != "" {
		target.User = in.User
	}
	if in.Password != "" {
		target.Password = in.Password
	}
	if in.PasswordEnv != "" {
		target.Password = os.Getenv(in.PasswordEnv)
	}
	if in.SSLMode != "" {
		target.SSLMode = in.SSLMode
	}
	if in.MaxConns > 0 {
		target.MaxConns = in.MaxConns
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
	cfg.DataStore.ReadSource = env("CLOTHP_READ_SOURCE", cfg.DataStore.ReadSource)
	cfg.DataStore.WriteMode = env("CLOTHP_WRITE_MODE", cfg.DataStore.WriteMode)
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
	/* 双库存储口径与密钥提供方（设计 3.1/3.2） */
	switch cfg.DataStore.ReadSource {
	case "secure", "plain":
	default:
		return fmt.Errorf("config data_store.read_source must be secure or plain")
	}
	switch cfg.DataStore.WriteMode {
	case "both", "plain_only", "secure_only":
	default:
		return fmt.Errorf("config data_store.write_mode must be both, plain_only or secure_only")
	}
	if cfg.SecureDatabase != nil {
		if cfg.SecureDatabase.Host == "" || cfg.SecureDatabase.Name == "" || cfg.SecureDatabase.User == "" || cfg.SecureDatabase.SSLMode == "" {
			return fmt.Errorf("config databases.secure host, name, user and sslmode are required")
		}
		/* 单写 + 对端读取会产生旧数据读取，必须显式允许降级（设计 3.2 启动校验） */
		degraded := (cfg.DataStore.WriteMode == "plain_only" && cfg.DataStore.ReadSource == "secure") ||
			(cfg.DataStore.WriteMode == "secure_only" && cfg.DataStore.ReadSource == "plain")
		if degraded && !cfg.DataStore.AllowDegradedRead {
			return fmt.Errorf("config read_source=%s with write_mode=%s requires data_store.allow_degraded_read=true", cfg.DataStore.ReadSource, cfg.DataStore.WriteMode)
		}
	}
	switch cfg.Crypto.KMSProvider {
	case "softhsm2", "cloud_kms", "local":
	default:
		return fmt.Errorf("config crypto.kms_provider must be softhsm2, cloud_kms or local")
	}
	if cfg.Crypto.KMSProvider == "softhsm2" {
		if cfg.Crypto.SoftHSM2.PKCS11ModuleEnv == "" || cfg.Crypto.SoftHSM2.PINEnv == "" || cfg.Crypto.SoftHSM2.TokenLabel == "" {
			return fmt.Errorf("config crypto.softhsm2 pkcs11_module_env, pin_env and token_label are required when kms_provider=softhsm2")
		}
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
