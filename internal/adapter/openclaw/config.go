package openclaw

import (
	"os"
	"time"

	"github.com/crabwise-ai/crabwise/internal/daemon"
)

type Config struct {
	Enabled                bool
	GatewayURL             string
	APITokenEnv            string
	APIToken               string
	SessionRefreshInterval time.Duration
	CorrelationWindow      time.Duration
}

func ConfigFromDaemon(cfg daemon.OpenClawConfig) Config {
	apiToken := ""
	if cfg.APITokenEnv != "" {
		apiToken = os.Getenv(cfg.APITokenEnv)
	}

	return Config{
		Enabled:                cfg.Enabled,
		GatewayURL:             cfg.GatewayURL,
		APITokenEnv:            cfg.APITokenEnv,
		APIToken:               apiToken,
		SessionRefreshInterval: cfg.SessionRefreshInterval.Duration(),
		CorrelationWindow:      cfg.CorrelationWindow.Duration(),
	}
}
