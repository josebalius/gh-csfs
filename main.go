package main

import (
	"context"
	"os"

	"github.com/josebalius/gh-csfs/internal/cmd"
	"github.com/josebalius/gh-csfs/internal/csfs"
)

func main() {
	ctx := context.Background()
	app := csfs.NewApp()
	cmd := cmd.New(app)
	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
