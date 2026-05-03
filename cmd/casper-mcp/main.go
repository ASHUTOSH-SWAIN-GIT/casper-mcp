package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/config"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/mcp"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/migrations"
)

func main() {
	if err := run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usage()
	}

	switch args[1] {
	case "migrate":
		return runMigrate(args[2:])
	case "ingest":
		return runIngest(ctx, args[2:])
	case "serve":
		return runServe(ctx, args[2:])
	default:
		return usage()
	}
}

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	configPath := fs.String("config", ".casper/config.yaml", "path to Casper config")
	migrationsDir := fs.String("migrations", "migrations", "path to migration files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	if err := migrations.Up(cfg.Database.URL, *migrationsDir); err != nil {
		return err
	}

	fmt.Println("migrations applied")
	return nil
}

func runIngest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	configPath := fs.String("config", ".casper/config.yaml", "path to Casper config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	pool, err := graph.Connect(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	summary, err := ingest.Run(ctx, cfg, graph.NewStore(pool))
	if err != nil {
		return err
	}

	fmt.Printf(
		"ingested %d state files, %d code modules, %d resources, %d dependencies\n",
		summary.StateFiles,
		summary.CodeModules,
		summary.Resources,
		summary.Dependencies,
	)
	return nil
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", ".casper/config.yaml", "path to Casper config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	pool, err := graph.Connect(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	return server.ServeStdio(mcpserver.New(graph.NewStore(pool)))
}

func usage() error {
	return fmt.Errorf("usage: casper-mcp <migrate|ingest|serve> --config .casper/config.yaml")
}
