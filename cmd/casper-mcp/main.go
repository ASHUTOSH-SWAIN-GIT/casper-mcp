package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
	mcpserver "github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/mcp"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
	regopkg "github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy/rego"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ui"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/workflow"
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
	case "init":
		return runInit(args[2:])
	case "serve":
		return runServe(ctx, args[2:])
	case "export":
		return runExport(ctx, args[2:])
	default:
		return usage()
	}
}


func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir    := fs.String("dir", ".", "directory to scan for Terraform files")
	htmlOut := fs.String("html", "", "path to write live-updated HTML graph; empty = no HTML output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}

	// Load cloud.aws up front so both describe_live_state (awsClient below)
	// and S3 backend state fetching share the same credentials.
	awsCfg, awsConfigured, awsErr := awslive.LoadConfig(absDir)
	if awsErr != nil {
		log.Printf("casper: cloud config warning: %v", awsErr)
	}

	// discoverRemotes scans .tf files for `terraform { backend "s3" {} }`
	// blocks and returns a StateSource per backend. Called fresh on each scan
	// so edits that add/remove backend blocks are picked up without a
	// server restart.
	discoverRemotes := func() []ingest.StateSource {
		backends, err := terraformcode.FindS3Backends(absDir)
		if err != nil {
			log.Printf("casper: backend discovery warning: %v", err)
			return nil
		}
		if len(backends) == 0 {
			return nil
		}
		log.Printf("casper: detected %d S3 backend(s)", len(backends))
		sources := make([]ingest.StateSource, 0, len(backends))
		for _, b := range backends {
			b := b
			sources = append(sources, ingest.StateSource{
				Type:       "s3",
				Identity:   fmt.Sprintf("s3://%s/%s", b.Bucket, b.Key),
				Bucket:     b.Bucket,
				Key:        b.Key,
				Region:     b.Region,
				DeclaredIn: b.Source,
				Fetch: func(ctx context.Context) ([]byte, error) {
					return awslive.FetchS3State(ctx, awsCfg, b.Bucket, b.Key, b.Region)
				},
			})
		}
		return sources
	}

	snapshot, statuses, err := ingest.ScanWithRemoteStates(ctx, absDir, discoverRemotes())
	if err != nil {
		return fmt.Errorf("scan %s: %w", absDir, err)
	}

	// stateSourceStatuses is kept fresh on every rescan so the
	// list_state_sources MCP tool always returns the latest fetch results.
	var stateSourceStatuses atomic.Pointer[[]ingest.StateSourceStatus]
	stateSourceStatuses.Store(&statuses)
	getStateSources := func() []ingest.StateSourceStatus {
		if p := stateSourceStatuses.Load(); p != nil {
			return *p
		}
		return nil
	}

	liveStore := graph.NewLiveStore(snapshot)

	// Pick the policy engine. If the repo has any .rego files anywhere we
	// treat them as the authoritative policy source and disable yaml policies
	// for this session. Otherwise we fall back to the legacy yaml engine
	// (no-op when .casper/policies.yaml is absent).
	engine, err := loadPolicyEngine(ctx, absDir)
	if err != nil {
		return fmt.Errorf("load policy engine: %w", err)
	}

	workflowRules, err := workflow.Load(absDir)
	if err != nil {
		log.Printf("casper: workflow rules load warning: %v", err)
	}

	simulate := func(code string) (*graph.ImpactResult, error) {
		return ingest.SimulateImpact(liveStore.Snapshot(), liveStore, engine, workflowRules, code)
	}

	var awsClient *awslive.Client
	if awsConfigured {
		if c, err := awslive.NewClient(ctx, awsCfg); err != nil {
			log.Printf("casper: AWS client init failed: %v", err)
		} else {
			awsClient = c
			log.Printf("casper: AWS client ready (role=%s, regions=%v)", awsCfg.RoleARN, awsCfg.Regions)
		}
	}

	// Resolve HTML output path (absolute) so deletions are detected reliably.
	// Resolved against absDir so the file lives inside the scanned repo, not
	// wherever the MCP client happened to spawn the subprocess from.
	var htmlPath string
	if *htmlOut != "" {
		if filepath.IsAbs(*htmlOut) {
			htmlPath = *htmlOut
		} else {
			htmlPath = filepath.Join(absDir, *htmlOut)
		}
	}

	// renderEnabled gates the watcher and ticker so the HTML is only written
	// once the agent explicitly calls render_graph. This makes the graph
	// strictly opt-in — typing /casper triggers it, otherwise nothing lands
	// on disk.
	var renderEnabled atomic.Bool

	renderHTML := func(ctx context.Context) (string, string, int, int, error) {
		if htmlPath == "" {
			return "", absDir, 0, 0, fmt.Errorf("html output not configured (server was started without --html)")
		}
		snap := liveStore.Snapshot()
		if err := ui.Export(snap, htmlPath); err != nil {
			return "", absDir, 0, 0, err
		}
		renderEnabled.Store(true)
		log.Printf("casper: wrote graph to %s (resources=%d edges=%d)", htmlPath, len(snap.Resources), len(snap.Dependencies))
		return htmlPath, absDir, len(snap.Resources), len(snap.Dependencies), nil
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

		// Tick periodically so a deleted HTML file gets recreated even when
		// no Terraform change is happening.
		var ticker *time.Ticker
		var tickerC <-chan time.Time
		if htmlPath != "" {
			ticker = time.NewTicker(5 * time.Second)
			tickerC = ticker.C
			defer ticker.Stop()
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
				fresh, freshStatuses, err := ingest.ScanWithRemoteStates(ctx, absDir, discoverRemotes())
				if err != nil {
					log.Printf("casper: rescan failed: %v", err)
					continue
				}
				stateSourceStatuses.Store(&freshStatuses)
				liveStore.Reload(fresh)
				log.Printf("casper: reloaded %d resources", len(fresh.Resources))
				// Only refresh the HTML if it has been rendered at least
				// once via render_graph. Until /casper is invoked we keep
				// the filesystem clean.
				if htmlPath != "" && renderEnabled.Load() {
					if err := ui.Export(fresh, htmlPath); err != nil {
						log.Printf("casper: html export failed: %v", err)
					}
				}
			case <-tickerC:
				if htmlPath == "" || !renderEnabled.Load() {
					continue
				}
				if _, err := os.Stat(htmlPath); err == nil {
					continue
				}
				// File missing after first render — recreate from current
				// in-memory snapshot.
				if err := ui.Export(liveStore.Snapshot(), htmlPath); err != nil {
					log.Printf("casper: html recreate failed: %v", err)
				} else {
					log.Printf("casper: recreated %s", htmlPath)
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

	mcpSrv := mcpserver.New(liveStore, simulate, awsClient, engine, renderHTML, getStateSources)
	log.Printf("casper: serving %s over stdio (graph render is lazy — triggered by render_graph / /casper)", absDir)
	return server.ServeStdio(mcpSrv)
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

func openBrowser(path string) {
	// file:// URL
	url := "file://" + path
	for _, cmd := range []string{"open", "xdg-open", "start"} {
		if err := exec.Command(cmd, url).Start(); err == nil {
			return
		}
	}
}

// casperCommand is the content written to ~/.claude/commands/casper.md
const casperCommand = `Use the casper MCP tools to build infrastructure context for this project.

$ARGUMENTS

Instructions:
- ALWAYS call render_graph first. This materializes casper/graph.html for the current repo (the file does not exist until you do this). The response includes the absolute path and the directory that was scanned — surface both to the user.
- After rendering, if a specific intent or resource was provided in $ARGUMENTS, call get_context with that intent to find relevant resources, dependencies, and examples.
- If no intent was provided, call dump_graph for a full snapshot, then summarise: total resources, resource types, any policy violations.
- Briefly describe what you found — resource names, types, dependencies, anything notable (drift, policy violations, workflow decisions).
- If the user wants to make a change, call simulate_impact with the proposed HCL before applying anything.
- After the first render_graph call, the file auto-updates whenever .tf files change in the scanned directory.
`

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	client := fs.String("client", "claude-code", "MCP client: claude-code | claude-desktop | cursor | codex")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch strings.ToLower(*client) {
	case "claude-code", "claude":
		return initClaudeCode()
	case "claude-desktop", "desktop":
		return initClaudeDesktop()
	case "cursor":
		return initCursor()
	case "codex":
		return initCodex()
	default:
		return fmt.Errorf("unknown --client %q (supported: claude-code, claude-desktop, cursor, codex)", *client)
	}
}

// initClaudeCode writes the /casper slash command and registers Casper at
// user scope via the Claude Code CLI. User scope only — project-scope
// .mcp.json files are intentionally not created so that the same MCP server
// always operates on the directory of the current Claude Code session.
func initClaudeCode() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	commandsDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		return fmt.Errorf("create commands dir: %w", err)
	}
	commandDest := filepath.Join(commandsDir, "casper.md")
	if err := os.WriteFile(commandDest, []byte(casperCommand), 0o644); err != nil {
		return fmt.Errorf("write command file: %w", err)
	}
	fmt.Printf("created %s\n", commandDest)

	if err := registerGlobalMCPServerWithClaude(); err != nil {
		return fmt.Errorf("register Claude Code MCP server: %w", err)
	}
	fmt.Println("run /casper in any Claude Code session — Casper operates on that session's directory")
	return nil
}

func initClaudeDesktop() error {
	dest, err := claudeDesktopConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := writeMCPJSON(dest); err != nil {
		return fmt.Errorf("write Claude Desktop config: %w", err)
	}
	fmt.Println("restart Claude Desktop, then ask the model to use the casper tools")
	return nil
}

// initCursor registers Casper in Cursor at user scope (~/.cursor/mcp.json)
// so a single config covers every Cursor project.
func initCursor() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dest := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create cursor config dir: %w", err)
	}
	if err := writeMCPJSON(dest); err != nil {
		return fmt.Errorf("write Cursor mcp.json: %w", err)
	}
	fmt.Println("restart Cursor — Casper is wired up at user scope")
	return nil
}

func initCodex() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}
	dest := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create codex config dir: %w", err)
	}
	if err := writeCodexTOML(dest); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Println("restart Codex CLI — Casper is registered as an MCP server")
	return nil
}

func claudeDesktopConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

// writeCodexTOML appends or updates the [mcp_servers.casper] block in a Codex
// config.toml file, preserving any other content already present.
func writeCodexTOML(dest string) error {
	exe := resolveExecutable()
	block := fmt.Sprintf(`[mcp_servers.casper]
command = %q
args = ["serve", "--dir", ".", "--html", "casper/graph.html"]
`, exe)

	existing, err := os.ReadFile(dest)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var out string
	if len(existing) == 0 {
		out = block
	} else if idx := strings.Index(string(existing), "[mcp_servers.casper]"); idx >= 0 {
		// Replace existing block: from header to next [section] or EOF.
		text := string(existing)
		end := len(text)
		if next := strings.Index(text[idx+len("[mcp_servers.casper]"):], "\n["); next >= 0 {
			end = idx + len("[mcp_servers.casper]") + next + 1
		}
		out = text[:idx] + block + text[end:]
	} else {
		// Append, ensuring blank line separator.
		text := string(existing)
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if !strings.HasSuffix(text, "\n\n") {
			text += "\n"
		}
		out = text + block
	}

	if err := os.WriteFile(dest, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Printf("updated %s\n", dest)
	return nil
}

// resolveExecutable returns the absolute path to this binary so that MCP
// clients (Claude Code, Cursor, etc.) can find it even when the directory
// it lives in (e.g. /opt/homebrew/bin) is not on their PATH.
func resolveExecutable() string {
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.EvalSymlinks(exe); err == nil {
			return abs
		}
		return exe
	}
	return "casper-mcp" // fallback
}

// writeMCPJSON writes the casper MCP server config to dest, preserving any
// existing servers already defined in that file.
func writeMCPJSON(dest string) error {
	existing := map[string]any{}
	if data, err := os.ReadFile(dest); err == nil {
		// Best-effort parse — ignore errors so a corrupted file is overwritten cleanly.
		_ = json.Unmarshal(data, &existing)
	}

	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["casper"] = map[string]any{
		"command": resolveExecutable(),
		"args":    []string{"serve", "--dir", ".", "--html", "casper/graph.html"},
	}
	existing["mcpServers"] = servers

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", dest)
	return nil
}

// registerGlobalMCPServerWithClaude adds Casper through Claude Code's own MCP
// CLI so the entry lands in the current user-scope config format.
func registerGlobalMCPServerWithClaude() error {
	config := map[string]any{
		"type":    "stdio",
		"command": resolveExecutable(),
		"args":    []string{"serve", "--dir", ".", "--html", "casper/graph.html"},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	cmd := exec.Command("claude", "mcp", "add-json", "casper", string(data), "--scope", "user")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s\ninstall with: claude mcp add-json casper '%s' --scope user", err, strings.TrimSpace(string(out)), data)
	}

	fmt.Println("registered casper in Claude Code user MCP scope")
	return nil
}

// loadPolicyEngine picks the right policy backend for the scanned repo.
// Auto-discovery: any .rego file under absDir → Rego engine (yaml ignored).
// No .rego files → YAML engine from .casper/policies.yaml (no-op if absent).
func loadPolicyEngine(ctx context.Context, absDir string) (policy.Engine, error) {
	regoFiles, err := regopkg.Discover(absDir)
	if err != nil {
		log.Printf("casper: rego discovery warning: %v", err)
	}

	if len(regoFiles) > 0 {
		e, err := regopkg.NewEngine(ctx, regoFiles)
		if err != nil {
			// User's .rego files don't compile — fail loud rather than
			// silently fall back to yaml. Broken policies should be visible.
			return nil, fmt.Errorf("compile rego policies: %w", err)
		}
		log.Printf("casper: loaded %d rego policies — yaml policies disabled", len(regoFiles))
		for _, f := range regoFiles {
			log.Printf("casper:   - %s", f.Path)
		}
		return e, nil
	}

	yamlPolicies, err := policy.Load(absDir)
	if err != nil {
		log.Printf("casper: yaml policy load warning: %v", err)
	}
	if len(yamlPolicies) > 0 {
		log.Printf("casper: loaded %d yaml policies", len(yamlPolicies))
	}
	return policy.NewYAMLEngine(yamlPolicies), nil
}

func usage() error {
	return fmt.Errorf("usage: casper-mcp <command> [flags]\n\nCommands:\n" +
		"  init    [--client <c>]\n" +
		"            Register Casper at user scope for an MCP client. Casper always operates\n" +
		"            on the directory of the active client session — never a different repo.\n" +
		"            --client claude-code     (default) ~/.claude.json + ~/.claude/commands/casper.md\n" +
		"            --client claude-desktop  ~/Library/Application Support/Claude/claude_desktop_config.json\n" +
		"            --client cursor          ~/.cursor/mcp.json\n" +
		"            --client codex           ~/.codex/config.toml ([mcp_servers.casper] block)\n" +
		"  serve   -dir <path> [-html <path>]\n" +
		"            Scan a Terraform directory and start the MCP server over stdio.\n" +
		"            Used by Claude Code, Cursor, and Claude Desktop via .mcp.json.\n\n" +
		"  export  -dir <path> -output <file>   Export graph to HTML")
}
