package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	cmdimg "github.com/jlrickert/cmd-img"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	err := cmdimg.Run(ctx, os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

}
