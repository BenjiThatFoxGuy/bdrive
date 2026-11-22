package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tgdrive/teldrive/cmd"
	"github.com/tgdrive/teldrive/internal/psdconv"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == psdconv.WorkerArg {
		os.Exit(psdconv.PreviewWorkerMain(os.Args[2:], os.Stdin, os.Stdout))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	if err := cmd.New().ExecuteContext(ctx); err != nil {
		panic(err)
	}
}
