package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

var version = "dev"
var commit = "unknown"
var buildTime = "unknown"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	switch command {
	case "version":
		if len(args) != 0 {
			fmt.Fprintln(stderr, "version does not accept arguments")
			return 2
		}
		fmt.Fprintf(stdout, "hydra-admin %s\ncommit: %s\nbuilt: %s\ngo: %s\n", version, commit, buildTime, runtime.Version())
		return 0
	case "hash-password":
		return runHashPassword(args, stdout, stderr)
	case "check":
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		path := flags.String("config", "", "configuration path")
		if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" {
			fmt.Fprintln(stderr, "usage: hydra-admin check --config /path/to/config.yaml")
			return 2
		}
		cfg, err := config.Load(*path)
		if err != nil {
			fmt.Fprintf(stderr, "configuration invalid: %v\n", err)
			return 1
		}
		if err := cfg.CheckWritablePaths(); err != nil {
			fmt.Fprintf(stderr, "local file check failed: %v\n", err)
			return 1
		}
		adapter, err := newHydraAdapter(cfg)
		if err != nil {
			fmt.Fprintln(stderr, "Hydra probe configuration invalid")
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Hydra.Timeout.Value())
		defer cancel()
		report := adapter.Report(ctx)
		output := struct {
			LocalChecks string `json:"local_checks"`
			hydra.Report
		}{LocalChecks: "ok", Report: report}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(output); err != nil {
			return 1
		}
		if report.Status != "supported" || report.HealthProbe != hydra.ProbeOK || report.ClientListProbe != hydra.ProbeOK {
			return 1
		}
		return 0
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		path := flags.String("config", "", "configuration path")
		if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" {
			fmt.Fprintln(stderr, "usage: hydra-admin serve --config /path/to/config.yaml")
			return 2
		}
		return runServe(*path, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", command)
		return 2
	}
}

func runHashPassword(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hash-password", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	algorithm := flags.String("algorithm", "argon2id", "argon2id or bcrypt")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || (*algorithm != "argon2id" && *algorithm != "bcrypt") {
		fmt.Fprintln(stderr, "usage: hydra-admin hash-password [--algorithm bcrypt]")
		return 2
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(stderr, "hash-password requires an interactive TTY")
		return 1
	}
	fmt.Fprint(stderr, "Password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		fmt.Fprintln(stderr, "unable to read password from TTY")
		return 1
	}
	defer clear(first)
	fmt.Fprint(stderr, "Repeat password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		fmt.Fprintln(stderr, "unable to read password from TTY")
		return 1
	}
	defer clear(second)
	if len(first) == 0 || subtle.ConstantTimeCompare(first, second) != 1 {
		fmt.Fprintln(stderr, "passwords do not match or are empty")
		return 1
	}
	hash, err := auth.GeneratePasswordHash(first, *algorithm)
	if err != nil {
		fmt.Fprintln(stderr, "unable to hash password")
		return 1
	}
	fmt.Fprintln(stdout, hash)
	return 0
}
