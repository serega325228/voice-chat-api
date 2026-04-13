package config

import (
	"log"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
)

type Config struct {
	Env        string `yaml:"env" env:"ENV" env-default:"development"`
	HTTPServer `yaml:"http_server"`
	Postgres   `yaml:"postgres"`
	JWT        `yaml:"jwt"`
}

type HTTPServer struct {
	Address         string        `yaml:"address" env-default:"localhost:8080"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env-default:"15s"`
	Timeout         time.Duration `yaml:"timeout" env-default:"4s"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

type Postgres struct {
	MaxConns          int32         `yaml:"max_conns" env-default:"15"`
	HealthcheckPeriod time.Duration `yaml:"health_check_period" env-default:"60s"`
}

type JWT struct {
	AccessTTL  time.Duration `yaml:"access_ttl" env-default:"15m"`
	RefreshTTL time.Duration `yaml:"refresh_ttl" env-default:"30d"`
}

type Secrets struct {
	JWTSecret   string `env:"JWT_SECRET_KEY" env-required:"true"`
	PostgresURL string `env:"POSTGRES_URL" env-required:"true"`
}

func MustLoadSecrets() *Secrets {
	if err := godotenv.Load(); err != nil {
		log.Fatal(".env file is not opened")
	}

	var secrets Secrets

	secrets.JWTSecret = os.Getenv("JWT_SECRET_KEY")

	return &secrets
}

func MustLoad() *Config {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH is not set")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("config file doesn't exist: %s", configPath)
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("cannot read config: %s", err)
	}

	return &cfg
}
