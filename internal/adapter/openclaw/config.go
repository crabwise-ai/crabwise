package openclaw

import (
	"os"
	"time"
)

type Config struct {
	Enabled                bool
	GatewayURL             string
	APITokenEnv            string
	APIToken               string
	SessionRefreshInterval time.Duration
	CorrelationWindow      time.Duration
}

func ResolveConfigEnv(cfg Config) Config {
	apiToken := ""
	if cfg.APITokenEnv != "" {
		apiToken = os.Getenv(cfg.APITokenEnv)
	}

	cfg.APIToken = apiToken
	return cfg
}
