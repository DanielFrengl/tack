package main

import (
	"os"

	"github.com/DanielFrengl/tack/internal/cli"
)

func main() {
	if cli.Execute() != nil {
		os.Exit(1)
	}
}
