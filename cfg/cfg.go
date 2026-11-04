package cfg

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
)

type Cfg struct {
	ServerPort string `env:"SERVER_PORT" envDefault:"8000"`

	OllamaHost     string `env:"OLLAMA_HOST" envDefault:"localhost"`
	OllamaPort     string `env:"OLLAMA_PORT" envDefault:"11434"`
	EmbeddingModel string `env:"EMBED_MODEL" envDefault:"nomic-embed-text"`
	ChatModel      string `env:"CHAT_MODEL" envDefault:"qwen3.5:9b"`
	ChatThink      bool   `env:"CHAT_THINK" envDefault:"false"` // on is slow
	ChatNumCtx     int    `env:"CHAT_NUM_CTX" envDefault:"16384"`

	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"localhost"`
	PostgresPort     string `env:"POSTGRES_PORT" envDefault:"5432"`
	PostgresUser     string `env:"POSTGRES_USER" envDefault:"postgres"`
	PostgresPassword string `env:"POSTGRES_PASSWORD"`
	PostgresName     string `env:"POSTGRES_NAME" envDefault:"pgvectordb"`

	TemporalHost      string `env:"TEMPORAL_HOST" envDefault:"localhost"`
	TemporalPort      string `env:"TEMPORAL_PORT" envDefault:"7233"`
	TemporalNamespace string `env:"TEMPORAL_NAMESPACE" envDefault:"default"`
	TemporalTaskQueue string `env:"TEMPORAL_TASK_QUEUE" envDefault:"golabagent"`
}

var Config Cfg

func Parse(envFile string) error {
	if err := godotenv.Load(envFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := env.Parse(&Config); err != nil {
		return err
	}

	return nil
}

func (c Cfg) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PostgresUser, c.PostgresPassword, c.PostgresHost, c.PostgresPort, c.PostgresName)
}

func (c Cfg) TemporalAddress() string {
	return c.TemporalHost + ":" + c.TemporalPort
}

func (c Cfg) OllamaURL() string {
	return fmt.Sprintf("http://%s:%s", c.OllamaHost, c.OllamaPort)
}
