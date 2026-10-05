package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
)

var version = "dev"

const usage = `terragraph converts Grafana dashboards to terragraph HCL.

Usage:
  terragraph convert [flags] FILE|DIR|- ...  convert dashboard JSON or YAML files
  terragraph pull [flags] [UID ...]         export dashboards from a Grafana instance
  terragraph migrate [flags] DIR            move grafana_dashboard resources to terragraph
  terragraph version

Run "terragraph <command> -h" for command flags.`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "convert":
		err = runConvert(args[1:], stdin, stdout, stderr)
	case "pull":
		err = runPull(ctx, args[1:], stdout, stderr)
	case "migrate":
		err = runMigrate(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version)
	case "help", "-h", "-help", "--help":
		fmt.Fprintln(stdout, usage)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
	var usageErr usageError
	switch {
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &usageErr):
		return 2
	case err != nil:
		fmt.Fprintln(stderr, "terragraph:", err)
		return 1
	}
	return 0
}

// usageError marks flag errors that the flag package has already reported.
type usageError struct{ error }

func parseFlags(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		return usageError{err}
	}
	return err
}
