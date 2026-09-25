package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	DatabaseURL string `env:"DATABASE_URL,required"`
	BaseURL     string `env:"BASE_URL,required"`
	HTTPAddr    string `env:"HTTP_ADDR" envDefault:":8080"`
}

// Load parses environment variables into Config.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
