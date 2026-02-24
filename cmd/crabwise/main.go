package main

import (
	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/cli"
	"github.com/crabwise-ai/crabwise/internal/daemon"
)

func init() {
	daemon.DefaultConfigYAML = configs.DefaultYAML
	daemon.DefaultCommandmentsYAML = configs.DefaultCommandmentsYAML
	daemon.DefaultToolRegistryYAML = configs.DefaultToolRegistryYAML
}

func main() {
	cli.Execute()
}
