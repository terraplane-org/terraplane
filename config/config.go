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
	OrchestratorGithubAccessToken   string        `mapstructure:"ORCHESTRATOR_GITHUB_ACCESS_TOKEN"`
	AgentClientShutdownTimer        time.Duration `mapstructure:"AGENT_CLIENT_SHUTDOWN_TIMER"`
	AgentOrchestratorURL            string        `mapstructure:"AGENT_ORCHESTRATOR_URL"`
	AgentID                         string        `mapstructure:"AGENT_ID"`
	AgentSCMSSHKeyPath              string        `mapstructure:"AGENT_SCM_SSH_KEY_PATH"`
	AgentWorkDir                    string        `mapstructure:"AGENT_WORK_DIR"`
	AgentTerraformBinDir            string        `mapstructure:"AGENT_TERRAFORM_BIN_DIR"`
	AgentDefaultTerraformVersion    string        `mapstructure:"AGENT_DEFAULT_TERRAFORM_VERSION"`
	DatabaseDriver                  string        `mapstructure:"DATABASE_DRIVER"`
	DatabaseURL                     string        `mapstructure:"DATABASE_URL"`
	SharedAuthToken                 string        `mapstructure:"SHARED_AUTH_TOKEN"`
}

func NewConfig() (*Config, error) {
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.SetConfigName(".env")

	err := viper.ReadInConfig()
	if err != nil && !configNotFound(err) {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
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
	viper.SetDefault("ORCHESTRATOR_GITHUB_ACCESS_TOKEN", "")
	viper.SetDefault("AGENT_CLIENT_SHUTDOWN_TIMER", "5s")
	viper.SetDefault("AGENT_ORCHESTRATOR_URL", "ws://orchestrator:8080/ws")
	viper.SetDefault("AGENT_ID", "")
	viper.SetDefault("AGENT_SCM_SSH_KEY_PATH", "")
	viper.SetDefault("AGENT_WORK_DIR", "")
	viper.SetDefault("AGENT_TERRAFORM_BIN_DIR", "")
	viper.SetDefault("AGENT_DEFAULT_TERRAFORM_VERSION", "1.15.6")
	viper.SetDefault("DATABASE_DRIVER", "postgres")
	viper.SetDefault("DATABASE_URL", "")
	viper.SetDefault("SHARED_AUTH_TOKEN", "")
}
