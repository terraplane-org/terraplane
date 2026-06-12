package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	LogLevel string `mapstructure:"LOG_LEVEL"`
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
}
