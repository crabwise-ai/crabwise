package configs

import _ "embed"

//go:embed default.yaml
var DefaultYAML []byte

//go:embed commandments_default.yaml
var DefaultCommandmentsYAML []byte

//go:embed tool_registry.yaml
var DefaultToolRegistryYAML []byte

//go:embed proxy_mappings/openai.yaml
var DefaultOpenAIProxyMappingYAML []byte
