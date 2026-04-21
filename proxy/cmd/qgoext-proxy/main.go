// Command qgoext-proxy is an LSP middleware that sits between a Zed editor
// (or any LSP client) and a real gopls binary. It forwards traffic in both
// directions and, for a small set of methods, enriches the responses with
// reference and implementation counts.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/jorgefuertes/qgoext/proxy/internal/proxy"
)

func main() {
	gopls := flag.String("gopls", "gopls", "path to the real gopls binary")
	flag.Parse()

	if err := run(*gopls, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "qgoext-proxy:", err)
		os.Exit(1)
	}
}

func run(goplsPath string, goplsArgs []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd := exec.CommandContext(ctx, goplsPath, goplsArgs...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("gopls stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("gopls stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gopls: %w", err)
	}

	p := proxy.New(os.Stdin, os.Stdout, stdout, stdin)
	runErr := p.Run(ctx)

	_ = stdin.Close()
	waitErr := cmd.Wait()

	if runErr != nil && runErr != context.Canceled {
		return runErr
	}
	return waitErr
}
