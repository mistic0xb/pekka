package main

import (
	"log"

	"github.com/mistic0xb/zapbot/cmd"
	"github.com/mistic0xb/zapbot/internal/logger"
)

func main() {
	if err := logger.Init(); err != nil {
		log.Fatal(err)
	}
	cmd.Execute()
}
