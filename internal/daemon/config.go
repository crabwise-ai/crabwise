package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfigYAML is the embedded default config.
// Set from configs package at init time.
var DefaultConfigYAML []byte

// DefaultCommandmentsYAML is the embedded default commandments file.
// Set from configs package at init time.
var DefaultCommandmentsYAML []byte

type Config struct {
	Daemon       DaemonConfig       `yaml:"daemon"`
	Discovery    DiscoveryConfig    `yaml:"discovery"`
	Adapters     AdaptersConfig     `yaml:"adapters"`
	Queue        QueueConfig        `yaml:"queue"`
	Audit        AuditConfig        `yaml:"audit"`
	Commandments CommandmentsConfig `yaml:"commandments"`
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
}

type LogWatcherConfig struct {
	Enabled              bool     `yaml:"enabled"`
	PollFallbackInterval Duration `yaml:"poll_fallback_interval"`
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
		cfg.Discovery.ProcessSignatures = []string{"claude"}
		home, _ := os.UserHomeDir()
		cfg.Discovery.LogPaths = []string{filepath.Join(home, ".claude", "projects")}
		cfg.Adapters.LogWatcher.Enabled = true
		cfg.Adapters.LogWatcher.PollFallbackInterval = Duration(30 * time.Second)
		cfg.Queue.Capacity = 10000
		cfg.Queue.BatchSize = 100
		cfg.Queue.FlushInterval = Duration(time.Second)
		cfg.Queue.Overflow = "block_with_timeout"
		cfg.Queue.BlockTimeout = Duration(100 * time.Millisecond)
		cfg.Audit.RetentionDays = 30
		cfg.Audit.HashAlgorithm = "sha256"
		cfg.Commandments.File = "~/.config/crabwise/commandments.yaml"
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
	return nil
}

func defaultCommandmentsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise", "commandments.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise", "commandments.yaml")
}
