package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	LogLevel                        string        `mapstructure:"LOG_LEVEL"`
	ServerShutdownTimer             time.Duration `mapstructure:"SERVER_SHUTDOWN_TIMER"`
	OrchestratorListenAddress       string        `mapstructure:"ORCHESTRATOR_LISTEN_ADDRESS"`
	OrchestratorListenPort          int           `mapstructure:"ORCHESTRATOR_LISTEN_PORT"`
	OrchestratorGithubWebhookSecret string        `mapstructure:"ORCHESTRATOR_GITHUB_WEBHOOK_SECRET"`
	AgentClientShutdownTimer        time.Duration `mapstructure:"AGENT_CLIENT_SHUTDOWN_TIMER"`
}

func NewConfig() (*Config, error) {
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.SetConfigName(".env")

	err := viper.ReadInConfig()
	if err != nil && !configNotFound(err) {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var c Config
	err = viper.Unmarshal(&c)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &c, nil
}

func configNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}

func init() {
	viper.AutomaticEnv()

	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("SERVER_SHUTDOWN_TIMER", "5s")
	viper.SetDefault("ORCHESTRATOR_LISTEN_ADDRESS", "127.0.0.1")
	viper.SetDefault("ORCHESTRATOR_LISTEN_PORT", 8080)
	viper.SetDefault("ORCHESTRATOR_GITHUB_WEBHOOK_SECRET", "")
	viper.SetDefault("AGENT_CLIENT_SHUTDOWN_TIMER", "5s")
}
