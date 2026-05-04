package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/config"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/mcp"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/migrations"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ui"
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
	case "ui":
		return runUI(ctx, args[2:])
	case "watch":
		return runWatch(ctx, args[2:])
	case "export":
		return runExport(ctx, args[2:])
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
	dir     := fs.String("dir", ".", "directory to scan for Terraform files")
	htmlOut := fs.String("html", "", "path to write live-updated HTML graph; empty = no HTML output")
	httpAddr := fs.String("http", "", "enable HTTP transport and listen on this address, e.g. :8080 (omit to use stdio)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}

	snapshot, err := ingest.Scan(absDir)
	if err != nil {
		return fmt.Errorf("scan %s: %w", absDir, err)
	}

	liveStore := graph.NewLiveStore(snapshot)

	policies, err := policy.Load(absDir)
	if err != nil {
		log.Printf("casper: policy load warning: %v", err)
	}

	simulate := func(code string) (*graph.ImpactResult, error) {
		return ingest.SimulateImpact(liveStore.Snapshot(), liveStore, policies, code)
	}

	var awsClient *awslive.Client
	if awsCfg, ok, err := awslive.LoadConfig(absDir); err != nil {
		log.Printf("casper: cloud config warning: %v", err)
	} else if ok {
		if c, err := awslive.NewClient(ctx, awsCfg); err != nil {
			log.Printf("casper: AWS client init failed: %v", err)
		} else {
			awsClient = c
			log.Printf("casper: AWS client ready (role=%s, regions=%v)", awsCfg.RoleARN, awsCfg.Regions)
		}
	}

	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Printf("casper: watcher init failed: %v", err)
			return
		}
		defer watcher.Close()

		if err := watchDirRecursive(watcher, absDir); err != nil {
			log.Printf("casper: watch dir failed: %v", err)
			return
		}

		var debounce <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if isTerraformFile(event.Name) {
					debounce = time.After(800 * time.Millisecond)
				}
			case <-debounce:
				debounce = nil
				fresh, err := ingest.Scan(absDir)
				if err != nil {
					log.Printf("casper: rescan failed: %v", err)
					continue
				}
				liveStore.Reload(fresh)
				log.Printf("casper: reloaded %d resources", len(fresh.Resources))
				if *htmlOut != "" {
					if err := ui.Export(fresh, *htmlOut); err != nil {
						log.Printf("casper: html export failed: %v", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("casper: watcher error: %v", err)
			case <-ctx.Done():
				return
			}
		}
	}()

	reloadFromGitHub := func(ctx context.Context, repoURL, token string) (int, int, error) {
		return cloneAndReload(ctx, repoURL, token, liveStore)
	}

	mcpSrv := mcpserver.New(liveStore, simulate, awsClient, policies, reloadFromGitHub)

	if *httpAddr != "" {
		httpSrv := server.NewStreamableHTTPServer(mcpSrv,
			server.WithEndpointPath("/mcp"),
			server.WithHeartbeatInterval(15*time.Second),
		)
		log.Printf("casper: serving %s over HTTP on %s/mcp (watching for changes)", absDir, *httpAddr)
		// Shut down the HTTP server cleanly when ctx is cancelled.
		go func() {
			<-ctx.Done()
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutCtx); err != nil {
				log.Printf("casper: HTTP shutdown error: %v", err)
			}
		}()
		return httpSrv.Start(*httpAddr)
	}

	log.Printf("casper: serving %s over stdio (watching for changes)", absDir)
	return server.ServeStdio(mcpSrv)
}

func runUI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	configPath := fs.String("config", ".casper/config.yaml", "path to Casper config")
	addr := fs.String("addr", ":8080", "http listen address")
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

	uiServer := ui.NewServer(graph.NewStore(pool))
	fmt.Printf("ui available at http://localhost%s\n", *addr)
	return http.ListenAndServe(*addr, uiServer.Handler())
}

func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	configPath := fs.String("config", ".casper/config.yaml", "path to Casper config")
	addr := fs.String("addr", ":8080", "http listen address")
	dir := fs.String("dir", ".", "directory to watch for Terraform file changes")
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

	store := graph.NewStore(pool)

	runIngestNow := func() {
		summary, err := ingest.Run(ctx, cfg, store)
		if err != nil {
			log.Printf("ingest error: %v", err)
			return
		}
		log.Printf("ingested %d state files, %d code modules, %d resources, %d dependencies",
			summary.StateFiles, summary.CodeModules, summary.Resources, summary.Dependencies)
	}

	runIngestNow()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watchDirRecursive(watcher, *dir); err != nil {
		return fmt.Errorf("watch dir: %w", err)
	}

	go func() {
		var debounce <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if isTerraformFile(event.Name) {
					debounce = time.After(800 * time.Millisecond)
				}
			case <-debounce:
				debounce = nil
				runIngestNow()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("watcher error: %v", err)
			case <-ctx.Done():
				return
			}
		}
	}()

	uiServer := ui.NewServer(store)
	fmt.Printf("casper watching %s, ui at http://localhost%s\n", *dir, *addr)
	return http.ListenAndServe(*addr, uiServer.Handler())
}

func watchDirRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == ".terraform" || name == "node_modules" {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}

func isTerraformFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return strings.HasSuffix(base, ".tf") ||
		strings.HasSuffix(base, ".tfstate") ||
		strings.HasSuffix(base, ".tfstate.backup")
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	dir := fs.String("dir", ".", "directory to scan for Terraform files")
	output := fs.String("output", "casper-graph.html", "output HTML file path")
	open := fs.Bool("open", true, "open the HTML file in the browser after export")
	if err := fs.Parse(args); err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}

	fmt.Printf("scanning %s...\n", absDir)
	snapshot, err := ingest.Scan(absDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	fmt.Printf("found %d resources, %d dependencies\n", len(snapshot.Resources), len(snapshot.Dependencies))

	absOut, err := filepath.Abs(*output)
	if err != nil {
		return err
	}

	if err := ui.Export(snapshot, absOut); err != nil {
		return fmt.Errorf("export: %w", err)
	}

	fmt.Printf("exported → %s\n", absOut)

	if *open {
		openBrowser(absOut)
	}
	return nil
}

// cloneAndReload clones a GitHub repo to a temp dir, scans it, and reloads the live store.
func cloneAndReload(ctx context.Context, repoURL, token string, liveStore *graph.LiveStore) (int, int, error) {
	// Build a stable temp dir name from the URL content so the same repo always
	// gets the same dir and different repos never collide.
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoURL))
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("casper-%x", h.Sum32()))

	// If the dir already exists, remove it so we get a clean clone.
	_ = os.RemoveAll(tmpDir)

	cloneURL := repoURL
	if token != "" {
		// Inject token into HTTPS URL: https://<token>@github.com/...
		cloneURL = strings.Replace(repoURL, "https://", "https://"+token+"@", 1)
	}

	log.Printf("casper: cloning %s → %s", repoURL, tmpDir)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, tmpDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, 0, fmt.Errorf("git clone failed: %w\n%s", err, out)
	}

	snapshot, err := ingest.Scan(tmpDir)
	if err != nil {
		return 0, 0, fmt.Errorf("scan %s: %w", tmpDir, err)
	}

	liveStore.Reload(snapshot)
	log.Printf("casper: loaded %d resources, %d deps from %s", len(snapshot.Resources), len(snapshot.Dependencies), repoURL)
	return len(snapshot.Resources), len(snapshot.Dependencies), nil
}

func openBrowser(path string) {
	// file:// URL
	url := "file://" + path
	for _, cmd := range []string{"open", "xdg-open", "start"} {
		if err := exec.Command(cmd, url).Start(); err == nil {
			return
		}
	}
}

func usage() error {
	return fmt.Errorf(`usage: casper-mcp <command> [flags]

Commands:
  serve   -dir <path> [-http <addr>] [-html <path>]
            Scan a Terraform directory and start the MCP server.
            Stdio mode (default): used by Claude Code, Cursor, Claude Desktop.
            HTTP mode (-http :8080): exposes POST/GET/DELETE /mcp for custom clients.

  ingest  -config <path>   Ingest Terraform into Postgres graph store.
  migrate -config <path>   Run database migrations.
  watch   -config <path> -dir <path> -addr <addr>  Watch + ingest + UI.
  ui      -config <path> -addr <addr>  Start the graph UI.
  export  -dir <path> -output <file>   Export graph to HTML.`)
}
