package main

import (
	"log"
	"os"

	"github.com/pomkita/pomkita-be/internal/config"
	"github.com/pomkita/pomkita-be/internal/httpapi"
)

func main() {
	cfg := config.Load()
	router := httpapi.NewRouter(cfg.Environment)

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
