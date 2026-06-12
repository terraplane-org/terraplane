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
	viper.SetConfigName(".env")

	err := viper.ReadInConfig()
	if err != nil && !configNotFound(err) {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var c Config
	err = viper.Unmarshal(&c)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal subject: %w", err)
	}

	return &c, nil
}

func configNotFound(err error) bool {
	return !errors.Is(err, viper.ConfigFileNotFoundError{}) && !errors.Is(err, &os.PathError{})
}

func init() {
	viper.AutomaticEnv()

	viper.SetDefault("LOG_LEVEL", "info")
}
