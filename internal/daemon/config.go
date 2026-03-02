package daemon

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/crabwise-ai/crabwise/internal/service"
	"gopkg.in/yaml.v3"
)

// DefaultConfigYAML is the embedded default config.
// Set from configs package at init time.
var DefaultConfigYAML []byte

// DefaultCommandmentsYAML is the embedded default commandments file.
// Set from configs package at init time.
var DefaultCommandmentsYAML []byte

// DefaultToolRegistryYAML is the embedded default tool registry file.
// Set from configs package at init time.
var DefaultToolRegistryYAML []byte

type Config struct {
	Daemon       DaemonConfig       `yaml:"daemon"`
	Discovery    DiscoveryConfig    `yaml:"discovery"`
	Adapters     AdaptersConfig     `yaml:"adapters"`
	Queue        QueueConfig        `yaml:"queue"`
	Audit        AuditConfig        `yaml:"audit"`
	Commandments CommandmentsConfig `yaml:"commandments"`
	ToolRegistry ToolRegistryConfig `yaml:"tool_registry"`
	Service      ServiceConfig      `yaml:"service"`
	OTel         OTelConfig         `yaml:"otel"`
}

type OTelConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Endpoint       string   `yaml:"endpoint"`
	ExportInterval Duration `yaml:"export_interval"`
}

type DaemonConfig struct {
	SocketPath    string `yaml:"socket_path"`
	DBPath        string `yaml:"db_path"`
	RawPayloadDir string `yaml:"raw_payload_dir"`
	LogLevel      string `yaml:"log_level"`
	PIDFile       string `yaml:"pid_file"`
}

type DiscoveryConfig struct {
	ScanInterval      Duration `yaml:"scan_interval"`
	ProcessSignatures []string `yaml:"process_signatures"`
	LogPaths          []string `yaml:"log_paths"`
}

type AdaptersConfig struct {
	LogWatcher LogWatcherConfig `yaml:"log_watcher"`
	Proxy      ProxyConfig      `yaml:"proxy"`
	OpenClaw   OpenClawConfig   `yaml:"openclaw"`
}

type LogWatcherConfig struct {
	Enabled              bool     `yaml:"enabled"`
	PollFallbackInterval Duration `yaml:"poll_fallback_interval"`
}

type ProxyConfig struct {
	Enabled             bool                           `yaml:"enabled"`
	Listen              string                         `yaml:"listen"`
	DefaultProvider     string                         `yaml:"default_provider"`
	UpstreamTimeout     Duration                       `yaml:"upstream_timeout"`
	StreamIdleTimeout   Duration                       `yaml:"stream_idle_timeout"`
	MaxRequestBody      int64                          `yaml:"max_request_body"`
	RedactEgressDefault bool                           `yaml:"redact_egress_default"`
	RedactPatterns      []string                       `yaml:"redact_patterns"`
	CACert              string                         `yaml:"ca_cert"`
	CAKey               string                         `yaml:"ca_key"`
	MappingsDir         string                         `yaml:"mappings_dir"`
	MappingStrictMode   bool                           `yaml:"mapping_strict_mode"`
	Providers           map[string]ProxyProviderConfig `yaml:"providers"`
}

type ProxyProviderConfig struct {
	UpstreamBaseURL string   `yaml:"upstream_base_url"`
	AuthMode        string   `yaml:"auth_mode"` // passthrough|configured
	AuthKey         string   `yaml:"auth_key"`
	RoutePatterns   []string `yaml:"route_patterns"`
	MaxIdleConns    int      `yaml:"max_idle_conns"`
	IdleConnTimeout Duration `yaml:"idle_conn_timeout"`
}

type OpenClawConfig struct {
	Enabled                bool     `yaml:"enabled"`
	GatewayURL             string   `yaml:"gateway_url"`
	APITokenEnv            string   `yaml:"api_token_env"`
	SessionRefreshInterval Duration `yaml:"session_refresh_interval"`
	CorrelationWindow      Duration `yaml:"correlation_window"`
}

type QueueConfig struct {
	Capacity      int      `yaml:"capacity"`
	BatchSize     int      `yaml:"batch_size"`
	FlushInterval Duration `yaml:"flush_interval"`
	Overflow      string   `yaml:"overflow"`
	BlockTimeout  Duration `yaml:"block_timeout"`
}

type AuditConfig struct {
	RetentionDays        int      `yaml:"retention_days"`
	HashAlgorithm        string   `yaml:"hash_algorithm"`
	RawPayloadEnabled    bool     `yaml:"raw_payload_enabled"`
	RawPayloadMaxSize    int64    `yaml:"raw_payload_max_size"`
	RawPayloadQuota      int64    `yaml:"raw_payload_quota"`
	RawPayloadGCInterval Duration `yaml:"raw_payload_gc_interval"`
}

type CommandmentsConfig struct {
	File string `yaml:"file"`
}

type ToolRegistryConfig struct {
	File string `yaml:"file"`
}

type ServiceConfig struct {
	Agents map[string]service.AgentServiceEntry `yaml:"agents"`
}

// Duration wraps time.Duration for YAML unmarshaling.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}

	// Load defaults
	if len(DefaultConfigYAML) > 0 {
		if err := yaml.Unmarshal(DefaultConfigYAML, cfg); err != nil {
			return nil, fmt.Errorf("parse default config: %w", err)
		}
	} else {
		// Hardcoded fallback defaults
		cfg.Daemon.SocketPath = "~/.local/share/crabwise/crabwise.sock"
		cfg.Daemon.DBPath = "~/.local/share/crabwise/crabwise.db"
		cfg.Daemon.RawPayloadDir = "~/.local/share/crabwise/raw/"
		cfg.Daemon.LogLevel = "info"
		cfg.Daemon.PIDFile = "~/.local/share/crabwise/crabwise.pid"
		cfg.Discovery.ScanInterval = Duration(10 * time.Second)
		cfg.Discovery.ProcessSignatures = []string{"claude", "codex"}
		home, _ := os.UserHomeDir()
		cfg.Discovery.LogPaths = []string{
			filepath.Join(home, ".claude", "projects"),
			filepath.Join(home, ".codex", "sessions"),
		}
		cfg.Adapters.LogWatcher.Enabled = true
		cfg.Adapters.LogWatcher.PollFallbackInterval = Duration(30 * time.Second)
		cfg.Adapters.Proxy.Enabled = true
		cfg.Adapters.Proxy.Listen = "127.0.0.1:9119"
		cfg.Adapters.Proxy.DefaultProvider = "openai"
		cfg.Adapters.Proxy.UpstreamTimeout = Duration(30 * time.Second)
		cfg.Adapters.Proxy.StreamIdleTimeout = Duration(120 * time.Second)
		cfg.Adapters.Proxy.MaxRequestBody = 10 * 1024 * 1024
		cfg.Adapters.Proxy.CACert = "~/.local/share/crabwise/ca.crt"
		cfg.Adapters.Proxy.CAKey = "~/.local/share/crabwise/ca.key"
		cfg.Adapters.Proxy.MappingsDir = "~/.config/crabwise/proxy_mappings"
		cfg.Adapters.Proxy.Providers = map[string]ProxyProviderConfig{
			"openai": {
				UpstreamBaseURL: "https://api.openai.com",
				AuthMode:        "passthrough",
				RoutePatterns:   []string{"/v1/chat/completions", "/v1/responses", "/v1/embeddings"},
				MaxIdleConns:    10,
				IdleConnTimeout: Duration(90 * time.Second),
			},
		}
		cfg.Adapters.OpenClaw.Enabled = false
		cfg.Adapters.OpenClaw.GatewayURL = "ws://127.0.0.1:18789"
		cfg.Adapters.OpenClaw.APITokenEnv = "OPENCLAW_API_TOKEN"
		cfg.Adapters.OpenClaw.SessionRefreshInterval = Duration(30 * time.Second)
		cfg.Adapters.OpenClaw.CorrelationWindow = Duration(3 * time.Second)
		cfg.Queue.Capacity = 10000
		cfg.Queue.BatchSize = 100
		cfg.Queue.FlushInterval = Duration(time.Second)
		cfg.Queue.Overflow = "block_with_timeout"
		cfg.Queue.BlockTimeout = Duration(100 * time.Millisecond)
		cfg.Audit.RetentionDays = 30
		cfg.Audit.HashAlgorithm = "sha256"
		cfg.Commandments.File = "~/.config/crabwise/commandments.yaml"
		cfg.ToolRegistry.File = "~/.config/crabwise/tool_registry.yaml"
	}

	// Override with user config if present
	if path == "" {
		path = defaultConfigPath()
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	cfg.expandPaths()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func defaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise", "config.yaml")
}

func (c *Config) expandPaths() {
	home, _ := os.UserHomeDir()
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}

	c.Daemon.SocketPath = expand(c.Daemon.SocketPath)
	c.Daemon.DBPath = expand(c.Daemon.DBPath)
	c.Daemon.RawPayloadDir = expand(c.Daemon.RawPayloadDir)
	c.Daemon.PIDFile = expand(c.Daemon.PIDFile)
	c.Commandments.File = expand(c.Commandments.File)
	c.ToolRegistry.File = expand(c.ToolRegistry.File)
	c.Adapters.Proxy.CACert = expand(c.Adapters.Proxy.CACert)
	c.Adapters.Proxy.CAKey = expand(c.Adapters.Proxy.CAKey)
	c.Adapters.Proxy.MappingsDir = expand(c.Adapters.Proxy.MappingsDir)

	for i, p := range c.Discovery.LogPaths {
		c.Discovery.LogPaths[i] = expand(p)
	}
}

func (c *Config) validate() error {
	if c.Daemon.SocketPath == "" {
		return fmt.Errorf("daemon.socket_path required")
	}
	if c.Daemon.DBPath == "" {
		return fmt.Errorf("daemon.db_path required")
	}
	if c.Commandments.File == "" {
		c.Commandments.File = defaultCommandmentsPath()
	}
	if c.ToolRegistry.File == "" {
		c.ToolRegistry.File = defaultToolRegistryPath()
	}
	if c.Adapters.Proxy.MappingsDir == "" {
		c.Adapters.Proxy.MappingsDir = defaultMappingsDirPath()
	}
	switch c.Daemon.LogLevel {
	case "debug", "info", "warn", "error", "":
	default:
		return fmt.Errorf("invalid log_level %q", c.Daemon.LogLevel)
	}
	if c.Queue.Capacity <= 0 {
		return fmt.Errorf("queue.capacity must be > 0")
	}
	switch c.Queue.Overflow {
	case "block_with_timeout", "drop_oldest", "":
	default:
		return fmt.Errorf("invalid queue.overflow %q", c.Queue.Overflow)
	}
	if c.Adapters.Proxy.Enabled {
		if strings.TrimSpace(c.Adapters.Proxy.Listen) == "" {
			return fmt.Errorf("adapters.proxy.listen required when proxy enabled")
		}
		if c.Adapters.Proxy.UpstreamTimeout.Duration() <= 0 {
			return fmt.Errorf("adapters.proxy.upstream_timeout must be > 0")
		}
		if c.Adapters.Proxy.StreamIdleTimeout.Duration() <= 0 {
			return fmt.Errorf("adapters.proxy.stream_idle_timeout must be > 0")
		}
		if c.Adapters.Proxy.MaxRequestBody <= 0 {
			return fmt.Errorf("adapters.proxy.max_request_body must be > 0")
		}
		if c.Adapters.Proxy.DefaultProvider == "" {
			return fmt.Errorf("adapters.proxy.default_provider required when proxy enabled")
		}
		if len(c.Adapters.Proxy.Providers) == 0 {
			return fmt.Errorf("adapters.proxy.providers required when proxy enabled")
		}
		if _, ok := c.Adapters.Proxy.Providers[c.Adapters.Proxy.DefaultProvider]; !ok {
			return fmt.Errorf("adapters.proxy.default_provider %q not found in providers", c.Adapters.Proxy.DefaultProvider)
		}
		for name, p := range c.Adapters.Proxy.Providers {
			if strings.TrimSpace(p.UpstreamBaseURL) == "" {
				return fmt.Errorf("adapters.proxy.providers.%s.upstream_base_url required", name)
			}
			switch p.AuthMode {
			case "", "passthrough", "configured":
			default:
				return fmt.Errorf("adapters.proxy.providers.%s.auth_mode invalid %q", name, p.AuthMode)
			}
			if p.AuthMode == "configured" && strings.TrimSpace(p.AuthKey) == "" {
				return fmt.Errorf("adapters.proxy.providers.%s.auth_key required when auth_mode=configured", name)
			}
			if len(p.RoutePatterns) == 0 && name != c.Adapters.Proxy.DefaultProvider {
				return fmt.Errorf("adapters.proxy.providers.%s.route_patterns required unless default provider", name)
			}
		}
		domainOwner := make(map[string]string)
		for name, p := range c.Adapters.Proxy.Providers {
			u, err := url.Parse(p.UpstreamBaseURL)
			if err == nil && u.Hostname() != "" {
				host := u.Hostname()
				if prev, dup := domainOwner[host]; dup {
					return fmt.Errorf("providers %q and %q share upstream domain %q; each domain must map to one provider", prev, name, host)
				}
				domainOwner[host] = name
			}
		}
		for i, pat := range c.Adapters.Proxy.RedactPatterns {
			if _, err := regexp.Compile(pat); err != nil {
				return fmt.Errorf("adapters.proxy.redact_patterns[%d] invalid regex %q: %w", i, pat, err)
			}
		}
	}
	if c.Adapters.OpenClaw.Enabled {
		if strings.TrimSpace(c.Adapters.OpenClaw.GatewayURL) == "" {
			return fmt.Errorf("adapters.openclaw.gateway_url required when openclaw enabled")
		}
		if c.Adapters.OpenClaw.SessionRefreshInterval.Duration() <= 0 {
			return fmt.Errorf("adapters.openclaw.session_refresh_interval must be > 0 when openclaw enabled")
		}
		if c.Adapters.OpenClaw.CorrelationWindow.Duration() <= 0 {
			return fmt.Errorf("adapters.openclaw.correlation_window must be > 0 when openclaw enabled")
		}
	}
	return nil
}

func defaultCommandmentsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise", "commandments.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise", "commandments.yaml")
}

func defaultToolRegistryPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise", "tool_registry.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise", "tool_registry.yaml")
}

func defaultMappingsDirPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise", "proxy_mappings")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise", "proxy_mappings")
}
