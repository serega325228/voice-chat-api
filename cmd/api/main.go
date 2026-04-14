package main

import (
	"log/slog"
	"voice-chat-api/internal/app"
)

func main() {
	a := app.New()

	if err := a.Run(); err != nil {
		slog.Error("application error", "err", err)
	}
}
