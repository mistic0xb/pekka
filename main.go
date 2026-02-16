package main

import (
	"log"

	"github.com/mistic0xb/pekka/cmd"
	"github.com/mistic0xb/pekka/internal/logger"
)

func main() {
	if err := logger.Init(); err != nil {
		log.Fatal(err)
	}
	cmd.Execute()
}
