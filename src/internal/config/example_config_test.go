package config

import (
	"path/filepath"
	"testing"
)

/* config.example.yaml / config.yaml 必须始终可解析并通过 validate：
 * 两份文件中的 crypto.softhsm2（远程 HSM 服务器连接配置）是生效配置，不允许被注释掉或写坏。 */
func TestShippedConfigFilesLoad(t *testing.T) {
	for _, name := range []string{"config.example.yaml", "config.yaml"} {
		path := filepath.Join("..", "..", name)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.Crypto.KMSProvider != "softhsm2" {
			t.Fatalf("%s: crypto.kms_provider=%q, want softhsm2", name, cfg.Crypto.KMSProvider)
		}
		if cfg.Crypto.SoftHSM2.ServerURL == "" {
			t.Fatalf("%s: crypto.softhsm2 server_url must not be empty/commented out", name)
		}
		if cfg.Crypto.SoftHSM2.APIKeyEnv == "" {
			t.Fatalf("%s: crypto.softhsm2 api_key_env must not be empty/commented out", name)
		}
		if cfg.Crypto.SoftHSM2.TokenLabel == "" || cfg.Crypto.SoftHSM2.KeyLabelPrefix == "" {
			t.Fatalf("%s: crypto.softhsm2 token_label/key_label_prefix must be set", name)
		}
	}
}
