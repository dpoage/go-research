package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dpoage/go-research/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	os.Exit(cmd.Run(ctx, os.Args[1:]))
}
