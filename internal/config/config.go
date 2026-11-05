// Package config 负责加载、展开与校验配置文件（见 dev_spec 9.9）。
//
// 优先级：默认值 → 配置文件 → 环境变量。敏感项（DSN、令牌、Provider 凭据）
// 只从环境变量读取，不写入配置文件。
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Role 决定校验哪些必填项：控制面需要存储，daemon 不需要。
type Role string

const (
	RoleServer Role = "server"
	RoleDaemon Role = "daemon"
)

// Duration 让 YAML 可以写 "30s" 这类字符串，并在解析时就拒绝非法值。
type Duration time.Duration

// UnmarshalYAML 实现 yaml.Unmarshaler。
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("时长必须是字符串（如 \"30s\"）: %w", err)
	}
	if strings.TrimSpace(s) == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// Std 返回标准库的 time.Duration。
func (d Duration) Std() time.Duration { return time.Duration(d) }

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Context   ContextConfig   `yaml:"context"`
	Storage   StorageConfig   `yaml:"storage"`
	Security  SecurityConfig  `yaml:"security"`
	Daemon    DaemonConfig    `yaml:"daemon"`
	ACP       ACPConfig       `yaml:"acp"`
}

type ServerConfig struct {
	HTTPAddr          string   `yaml:"http_addr"`
	WSPath            string   `yaml:"ws_path"`
	HeartbeatInterval Duration `yaml:"heartbeat_interval"`
	HeartbeatTimeout  Duration `yaml:"heartbeat_timeout"`
}

type SchedulerConfig struct {
	MaxActiveStepsPerTask int      `yaml:"max_active_steps_per_task"`
	TaskLockTTL           Duration `yaml:"task_lock_ttl"`
	TaskLockRenew         Duration `yaml:"task_lock_renew"`
	AttemptLeaseTTL       Duration `yaml:"attempt_lease_ttl"`
	RecoveryScanInterval  Duration `yaml:"recovery_scan_interval"`
	MaxAttemptsPerStep    int      `yaml:"max_attempts_per_step"`
}

type ContextConfig struct {
	ToolOutputMaxBytes        int  `yaml:"tool_output_max_bytes"`
	ReserveOutputTokens       int  `yaml:"reserve_output_tokens"`
	SummaryOnlyWhenDependency bool `yaml:"summary_only_when_dependency"`
}

type StorageConfig struct {
	MySQLDSN     string `yaml:"mysql_dsn"`
	RedisAddr    string `yaml:"redis_addr"`
	ArtifactRoot string `yaml:"artifact_root"`
}

type SecurityConfig struct {
	MachineTokenTTL Duration `yaml:"machine_token_ttl"`
	LogRedaction    bool     `yaml:"log_redaction"`
}

type DaemonConfig struct {
	ServerURL  string `yaml:"server_url"`
	ProfileDir string `yaml:"profile_dir"`
}

type ACPConfig struct {
	InitializeTimeout Duration `yaml:"initialize_timeout"`
	MaxMessageBytes   int64    `yaml:"max_message_bytes"`
}

// Default 返回带默认值的配置；Load 在此之上覆盖文件与环境变量。
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPAddr:          ":8080",
			WSPath:            "/v1/acp",
			HeartbeatInterval: Duration(30 * time.Second),
			HeartbeatTimeout:  Duration(90 * time.Second),
		},
		Scheduler: SchedulerConfig{
			MaxActiveStepsPerTask: 1,
			TaskLockTTL:           Duration(30 * time.Second),
			TaskLockRenew:         Duration(10 * time.Second),
			AttemptLeaseTTL:       Duration(60 * time.Second),
			RecoveryScanInterval:  Duration(30 * time.Second),
			MaxAttemptsPerStep:    3,
		},
		Context: ContextConfig{
			ToolOutputMaxBytes:        32768,
			ReserveOutputTokens:       4096,
			SummaryOnlyWhenDependency: true,
		},
		Storage: StorageConfig{
			ArtifactRoot: "./data/artifacts",
		},
		Security: SecurityConfig{
			MachineTokenTTL: Duration(24 * time.Hour),
			LogRedaction:    true,
		},
		Daemon: DaemonConfig{
			ProfileDir: "~/.aladdin/profiles",
		},
		ACP: ACPConfig{
			InitializeTimeout: Duration(3 * time.Second),
			MaxMessageBytes:   1 << 20,
		},
	}
}

// Load 按控制面角色加载配置。
func Load(path string) (*Config, error) {
	return LoadFor(path, RoleServer)
}

// LoadFor 按指定角色加载配置。
func LoadFor(path string, role Role) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s: %w", path, err)
	}
	cfg, err := Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.ValidateFor(role); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse 解析 YAML 文本（含 ${VAR} 展开）并做基础校验之外的结构检查。
func Parse(raw string) (*Config, error) {
	cfg := Default()
	expanded := os.ExpandEnv(raw)
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true) // 拼错的字段名必须报错，而不是静默忽略
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("解析 YAML: %w", err)
	}
	applyEnv(cfg)
	return cfg, nil
}

// applyEnv 让关键项可以被环境变量直接覆盖（优先级最高）。
func applyEnv(cfg *Config) {
	overrides := []struct {
		env    string
		target *string
	}{
		{"MYSQL_DSN", &cfg.Storage.MySQLDSN},
		{"REDIS_ADDR", &cfg.Storage.RedisAddr},
		{"ARTIFACT_ROOT", &cfg.Storage.ArtifactRoot},
		{"ALADDIN_SERVER_URL", &cfg.Daemon.ServerURL},
		{"ALADDIN_HTTP_ADDR", &cfg.Server.HTTPAddr},
	}
	for _, o := range overrides {
		if v := os.Getenv(o.env); v != "" {
			*o.target = v
		}
	}
}

// Validate 按控制面角色校验。
func (c *Config) Validate() error { return c.ValidateFor(RoleServer) }

// ValidateFor 校验指定角色必须满足的约束；错误信息包含字段路径。
func (c *Config) ValidateFor(role Role) error {
	var errs []error
	require := func(path, value string) {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, fmt.Errorf("%s: 必填", path))
		}
	}
	positive := func(path string, v time.Duration) {
		if v <= 0 {
			errs = append(errs, fmt.Errorf("%s: 必须大于 0", path))
		}
	}

	require("server.http_addr", c.Server.HTTPAddr)
	require("server.ws_path", c.Server.WSPath)
	positive("server.heartbeat_interval", c.Server.HeartbeatInterval.Std())
	positive("server.heartbeat_timeout", c.Server.HeartbeatTimeout.Std())
	if c.Server.HeartbeatTimeout.Std() <= c.Server.HeartbeatInterval.Std() {
		errs = append(errs, fmt.Errorf("server.heartbeat_timeout: 必须大于 heartbeat_interval"))
	}

	if c.Scheduler.MaxActiveStepsPerTask < 1 {
		errs = append(errs, fmt.Errorf("scheduler.max_active_steps_per_task: 必须 >= 1"))
	}
	if c.Scheduler.MaxAttemptsPerStep < 1 {
		errs = append(errs, fmt.Errorf("scheduler.max_attempts_per_step: 必须 >= 1"))
	}
	positive("scheduler.task_lock_ttl", c.Scheduler.TaskLockTTL.Std())
	positive("scheduler.task_lock_renew", c.Scheduler.TaskLockRenew.Std())
	if c.Scheduler.TaskLockRenew.Std() >= c.Scheduler.TaskLockTTL.Std() {
		errs = append(errs, fmt.Errorf("scheduler.task_lock_renew: 必须小于 task_lock_ttl"))
	}
	positive("scheduler.attempt_lease_ttl", c.Scheduler.AttemptLeaseTTL.Std())
	positive("scheduler.recovery_scan_interval", c.Scheduler.RecoveryScanInterval.Std())

	if c.Context.ToolOutputMaxBytes <= 0 {
		errs = append(errs, fmt.Errorf("context.tool_output_max_bytes: 必须大于 0"))
	}
	if c.Context.ReserveOutputTokens < 0 {
		errs = append(errs, fmt.Errorf("context.reserve_output_tokens: 不能为负数"))
	}

	positive("security.machine_token_ttl", c.Security.MachineTokenTTL.Std())
	positive("acp.initialize_timeout", c.ACP.InitializeTimeout.Std())
	if c.ACP.MaxMessageBytes <= 0 {
		errs = append(errs, fmt.Errorf("acp.max_message_bytes: 必须大于 0"))
	}

	if role == RoleServer {
		require("storage.mysql_dsn", c.Storage.MySQLDSN)
		require("storage.redis_addr", c.Storage.RedisAddr)
		require("storage.artifact_root", c.Storage.ArtifactRoot)
	}

	require("daemon.profile_dir", c.Daemon.ProfileDir)
	if role == RoleDaemon {
		require("daemon.server_url", c.Daemon.ServerURL)
	}

	return errors.Join(errs...)
}
