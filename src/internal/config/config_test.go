package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSMSProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `mode: center
server:
  listen: ":18370"
node:
  id: test-node
storage:
  attachment_dir: data/attachments
database:
  host: localhost
  port: 5432
  name: clothpm
  user: postgres
  password: test
  sslmode: disable
  max_conns: 20
sms:
  failure_threshold: 2
  cooldown_seconds: 30
  usage_file: "-"
  providers:
    - name: aliyun
      url: http://adapter-a/send
      weight: 100
      daily_limit: 50
      enabled: true
      extra:
        template_code: SMS_001
    - name: tencent
      url: http://adapter-b/send
      enabled: false
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SMS.Providers) != 2 {
		t.Fatalf("expected 2 sms providers, got %d", len(cfg.SMS.Providers))
	}
	if cfg.SMS.FailureThreshold != 2 || cfg.SMS.CooldownSeconds != 30 || cfg.SMS.UsageFile != "-" {
		t.Fatalf("sms settings not applied: %+v", cfg.SMS)
	}
	first := cfg.SMS.Providers[0]
	if first.Name != "aliyun" || first.DailyLimit != 50 || first.Extra["template_code"] != "SMS_001" {
		t.Fatalf("first provider mis-parsed: %+v", first)
	}
	second := cfg.SMS.Providers[1]
	if second.Enabled == nil || *second.Enabled {
		t.Fatalf("second provider should be explicitly disabled: %+v", second)
	}
}
