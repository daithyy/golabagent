package cfg

import (
	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
)

type Cfg struct {
	ServerPort     string `env:"SERVER_PORT"`
	OllamaPort     string `env:"OLLAMA_PORT"`
	EmbeddingModel string `env:"EMBED_MODEL"`
	ChatModel      string `env:"CHAT_MODEL"`
	PostgresPort   string `env:"POSTGRES_PORT"`
}

var Config Cfg

func Parse() error {
	err := godotenv.Load()
	if err != nil {
		return err
	}

	if err := env.Parse(&Config); err != nil {
		return err
	}

	return nil
}
