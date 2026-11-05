package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clearEnv 隔离环境变量，避免本机 MYSQL_DSN 等影响测试。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MYSQL_DSN", "REDIS_ADDR", "ARTIFACT_ROOT",
		"ALADDIN_SERVER_URL", "ALADDIN_HTTP_ADDR",
	} {
		t.Setenv(k, "")
	}
}

const validYAML = `
server:
  http_addr: ":9090"
  ws_path: "/v1/acp"
  heartbeat_interval: 20s
  heartbeat_timeout: 60s
scheduler:
  max_active_steps_per_task: 1
  task_lock_ttl: 30s
  task_lock_renew: 10s
  attempt_lease_ttl: 60s
  recovery_scan_interval: 30s
  max_attempts_per_step: 3
context:
  tool_output_max_bytes: 32768
  reserve_output_tokens: 4096
  summary_only_when_dependency: true
storage:
  mysql_dsn: "user:pass@tcp(127.0.0.1:3306)/aladdin?parseTime=true&loc=UTC"
  redis_addr: "127.0.0.1:6379"
  artifact_root: "./data/artifacts"
security:
  machine_token_ttl: 24h
  log_redaction: true
daemon:
  server_url: "ws://127.0.0.1:8080/v1/acp"
  profile_dir: "~/.aladdin/profiles"
acp:
  initialize_timeout: 3s
  max_message_bytes: 1048576
`

func TestValidateMissingDSN(t *testing.T) {
	clearEnv(t)
	cfg, err := Parse(strings.Replace(validYAML,
		`  mysql_dsn: "user:pass@tcp(127.0.0.1:3306)/aladdin?parseTime=true&loc=UTC"`,
		`  mysql_dsn: ""`, 1))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("期望校验失败，实际通过")
	}
	if !strings.Contains(err.Error(), "storage.mysql_dsn") {
		t.Fatalf("错误信息应包含字段路径 storage.mysql_dsn，实际: %v", err)
	}
}

func TestLoadFull(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.HTTPAddr != ":9090" {
		t.Errorf("http_addr = %q", cfg.Server.HTTPAddr)
	}
	if got := cfg.Scheduler.TaskLockTTL.Std(); got != 30*time.Second {
		t.Errorf("task_lock_ttl = %v", got)
	}
	if got := cfg.Security.MachineTokenTTL.Std(); got != 24*time.Hour {
		t.Errorf("machine_token_ttl = %v", got)
	}
	if cfg.ACP.MaxMessageBytes != 1<<20 {
		t.Errorf("max_message_bytes = %d", cfg.ACP.MaxMessageBytes)
	}
}

func TestDefaultsWhenOmitted(t *testing.T) {
	clearEnv(t)
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Errorf("默认 http_addr = %q", cfg.Server.HTTPAddr)
	}
	if cfg.Scheduler.MaxAttemptsPerStep != 3 {
		t.Errorf("默认 max_attempts_per_step = %d", cfg.Scheduler.MaxAttemptsPerStep)
	}
	if cfg.Security.AdminUserID != 1 {
		t.Errorf("默认 admin_user_id = %d", cfg.Security.AdminUserID)
	}
	// 缺 storage 时控制面校验必须失败
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "storage.mysql_dsn") {
		t.Fatalf("期望缺 DSN 校验失败，实际: %v", err)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("MYSQL_DSN", "env:env@tcp(db:3306)/x?parseTime=true")
	t.Setenv("REDIS_ADDR", "redis:6379")

	cfg, err := Parse(validYAML)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Storage.MySQLDSN != "env:env@tcp(db:3306)/x?parseTime=true" {
		t.Errorf("环境变量未覆盖 DSN: %q", cfg.Storage.MySQLDSN)
	}
	if cfg.Storage.RedisAddr != "redis:6379" {
		t.Errorf("环境变量未覆盖 Redis: %q", cfg.Storage.RedisAddr)
	}
}

func TestDaemonRoleDoesNotRequireStorage(t *testing.T) {
	clearEnv(t)
	raw := `
daemon:
  server_url: "ws://127.0.0.1:8080/v1/acp"
  profile_dir: "~/.aladdin/profiles"
`
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := cfg.ValidateFor(RoleDaemon); err != nil {
		t.Fatalf("daemon 角色不应要求 storage: %v", err)
	}
	if err := cfg.ValidateFor(RoleServer); err == nil {
		t.Fatal("控制面角色应当要求 storage")
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	clearEnv(t)
	_, err := Parse("server:\n  htttp_addr: \":8080\"\n")
	if err == nil {
		t.Fatal("拼错的字段名应被拒绝")
	}
	if !strings.Contains(err.Error(), "htttp_addr") {
		t.Fatalf("错误信息应指出未知字段，实际: %v", err)
	}
}

func TestInvalidDuration(t *testing.T) {
	clearEnv(t)
	_, err := Parse("server:\n  heartbeat_interval: \"30 seconds\"\n")
	if err == nil {
		t.Fatal("非法时长应被拒绝")
	}
}

func TestHeartbeatWindow(t *testing.T) {
	clearEnv(t)
	cfg, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.HeartbeatInterval = Duration(90 * time.Second)
	cfg.Server.HeartbeatTimeout = Duration(30 * time.Second)
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "heartbeat_timeout") {
		t.Fatalf("期望 heartbeat 窗口校验失败，实际: %v", err)
	}
}
