package configs

import _ "embed"

//go:embed default.yaml
var DefaultYAML []byte

//go:embed commandments_default.yaml
var DefaultCommandmentsYAML []byte
