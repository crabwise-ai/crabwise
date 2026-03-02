package service

// ProxyEnvVars returns the canonical set of proxy environment variables.
// Single source of truth used by both crabwise wrap and crabwise service inject.
func ProxyEnvVars(cfg EnvConfig) []EnvVar {
	vars := []EnvVar{
		{Key: "HTTPS_PROXY", Value: cfg.ProxyURL},
		{Key: "HTTP_PROXY", Value: cfg.ProxyURL},
		{Key: "ALL_PROXY", Value: cfg.ProxyURL},
		{Key: "https_proxy", Value: cfg.ProxyURL},
		{Key: "http_proxy", Value: cfg.ProxyURL},
		{Key: "all_proxy", Value: cfg.ProxyURL},
		{Key: "NO_PROXY", Value: "localhost,127.0.0.1"},
		{Key: "no_proxy", Value: "localhost,127.0.0.1"},
	}
	if cfg.CACert != "" {
		vars = append(vars, EnvVar{Key: "NODE_EXTRA_CA_CERTS", Value: cfg.CACert})
	}
	return vars
}
