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
	GRPC       `yaml:"grpc"`
	Signaling  `yaml:"signaling"`
	Postgres   `yaml:"postgres"`
	JWT        `yaml:"jwt"`
}

type HTTPServer struct {
	Address         string        `yaml:"address" env-default:"localhost:8080"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env-default:"15s"`
	CloseTimeout    time.Duration `yaml:"close_timeout" env-default:"10s"`
	Timeout         time.Duration `yaml:"timeout" env-default:"4s"`
	IdleTimeout     time.Duration `yaml:"idle_timeout" env-default:"60s"`
}

type GRPC struct {
	Address   string        `yaml:"address" env-default:"localhost:8081"`
	Keepalive GRPCKeepalive `yaml:"keepalive"`
}

type GRPCKeepalive struct {
	Time                time.Duration `yaml:"time" env-default:"20s"`
	Timeout             time.Duration `yaml:"timeout" env-default:"10s"`
	PermitWithoutStream bool          `yaml:"permit_without_stream" env-default:"true"`
}

type Signaling struct {
	EnqueueTimeout       time.Duration `yaml:"enqueue_timeout" env-default:"200ms"`
	ReconnectMinDelay    time.Duration `yaml:"reconnect_min_delay" env-default:"250ms"`
	ReconnectMaxDelay    time.Duration `yaml:"reconnect_max_delay" env-default:"5s"`
	LeaveTimeout         time.Duration `yaml:"leave_timeout" env-default:"5s"`
	ControlWriteTimeout  time.Duration `yaml:"control_write_timeout" env-default:"1s"`
	WebSocketSendBufSize int           `yaml:"websocket_send_buffer_size" env-default:"128"`
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
	var secrets Secrets
	_ = godotenv.Load()

	if err := cleanenv.ReadEnv(&secrets); err != nil {
		log.Fatalf("cannot read secrets from env: %s", err)
	}

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
