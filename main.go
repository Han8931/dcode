// Command dcode ("decode") is an interactive, AI-powered code-explaining app. It
// runs as a terminal app (the default) or a local web app (the "serve"
// subcommand); both drive the same vault and AI engine.
//
// The terminal UI splits the screen into three panes:
//
//	files (left)    -> the code/notes tree
//	editor (center) -> the in-app Vim/default editor (source files are read-only)
//	chat (right)    -> explanations and an interactive, code-grounded chat
//
// All AI calls and checks happen asynchronously so the UI stays responsive.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"dcode/internal/config"
	"dcode/internal/core"
	"dcode/internal/tui"
	"dcode/internal/tutor"
	"dcode/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	// Subcommand dispatch: "serve" launches the web UI, "check" the provider
	// diagnostic; anything else is the TUI. We peel the subcommand off os.Args
	// before flag parsing so each mode owns its own flag set.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			return runServe(os.Args[2:])
		case "check":
			return runCheck(os.Args[2:])
		}
	}
	return runTUI()
}

// buildService opens the project to decode and returns the engine over it. The
// project is chosen most-specific first: a command-line path argument, then the
// configured [vault] dir, then — with neither given — the current working
// directory, so running `dcode` inside a repo just decodes that repo. The chosen
// directory must already exist (a mistyped path is reported, never created), and
// the opened project is remembered for the in-app recent-projects picker.
func buildService(cfg config.Config, cliPath string) (*core.Service, error) {
	var codeDir string
	switch {
	case cliPath != "":
		abs, err := config.ResolveDir(cliPath)
		if err != nil {
			return nil, err
		}
		codeDir = abs
	case cfg.Vault.Dir != "":
		codeDir = cfg.VaultDir
	default:
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		codeDir = wd
	}
	svc, err := core.OpenProject(codeDir, cfg.NotesDir, tutor.New(cfg.AI), false)
	if err != nil {
		return nil, fmt.Errorf("%w\n  pass a project path or set [vault] dir in config.toml", err)
	}
	_, _ = config.AddRecent(svc.ProjectRoot())
	return svc, nil
}

// loadConfig loads config rooted at the working directory.
func loadConfig(cfgPath string) (config.Config, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config.Config{}, "", err
	}
	cfg, err := config.Load(cfgPath, wd)
	return cfg, wd, err
}

func runTUI() error {
	var (
		cfgPath = flag.String("config", "config.toml", "path to config file")
		vimFlag = flag.Bool("vim", false, "force Vim keybindings in the editor")
		defFlag = flag.Bool("default", false, "force default (non-Vim) keybindings")
	)
	flag.Parse()
	if flag.NArg() > 1 {
		return fmt.Errorf("too many arguments; usage: dcode [flags] [project-path]")
	}

	cfg, _, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	// CLI flags override the config file (most-specific wins).
	if *vimFlag {
		cfg.Editor.Keybindings = "vim"
	}
	if *defFlag {
		cfg.Editor.Keybindings = "default"
	}

	svc, err := buildService(cfg, flag.Arg(0))
	if err != nil {
		return err
	}
	_, err = tui.RunVault(svc, cfg)
	return err
}

// runServe starts the local web UI over the same vault and AI client as the TUI.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "config.toml", "path to config file")
	addr := fs.String("addr", ":8765", "address to listen on")
	_ = fs.Parse(args)

	cfg, _, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}

	svc, err := buildService(cfg, fs.Arg(0))
	if err != nil {
		return err
	}

	fmt.Printf("d-code web UI on http://localhost%s  (project: %s)\n", *addr, svc.ProjectRoot())
	if svc.Offline() {
		fmt.Println("(offline — no AI provider configured; set OPENAI_API_KEY or use Ollama for AI explanations)")
	}
	return web.Serve(*addr, svc)
}

// runCheck diagnoses the AI provider connection: resolved settings, whether the
// configured model exists upstream, and a real round-trip request.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	cfgPath := fs.String("config", "config.toml", "path to config file")
	_ = fs.Parse(args)

	cfg, _, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	tut := tutor.New(cfg.AI)
	info := tut.Info()

	fmt.Println("d-code AI connection check")
	fmt.Printf("  provider:  %s\n", cfg.AI.Provider)
	fmt.Printf("  base url:  %s\n", info.BaseURL)
	fmt.Printf("  model:     %s\n", info.Model)
	if cfg.AI.Provider == "compatible" && cfg.AI.BaseURL == "" {
		fmt.Println("  ⚠ provider is \"compatible\" but base_url is NOT set — defaulting to the")
		fmt.Println("    official OpenAI endpoint, which is probably not your gateway. Set")
		fmt.Println("    base_url = \"https://your-gateway/v1\" (the /v1 path usually matters).")
	}
	keyState := "not set"
	switch {
	case cfg.AI.APIKeyEnv != "" && os.Getenv(cfg.AI.APIKeyEnv) != "":
		keyState = "set (from $" + cfg.AI.APIKeyEnv + ")"
	case info.KeySet:
		keyState = "set (from api_key in config.toml)"
	case cfg.AI.APIKeyEnv != "":
		keyState = "not set (looked in $" + cfg.AI.APIKeyEnv + " and config api_key)" +
			"\n             note: api_key_env is the NAME of an env var, not the key itself"
	}
	fmt.Printf("  api key:   %s\n", keyState)

	if info.Offline {
		fmt.Println("\n✗ OFFLINE: this endpoint requires an API key and none is set.")
		fmt.Println("  Either `export " + cfg.AI.APIKeyEnv + "=sk-...` in the shell you run dcode from,")
		fmt.Println("  or put `api_key = \"sk-...\"` under [ai] in config.toml,")
		fmt.Println("  or point [ai] at a local provider (Ollama).")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Does the configured model exist upstream?
	models, err := tut.Models(ctx)
	switch {
	case err != nil:
		fmt.Printf("\n✗ could not reach the provider: %v\n", err)
		fmt.Println("  Is the server running and the base url correct?")
		return nil
	default:
		found := false
		for _, id := range models {
			if id == info.Model {
				found = true
				break
			}
		}
		if found {
			fmt.Printf("\n✓ provider reachable; model %q is available (%d models total)\n", info.Model, len(models))
		} else {
			fmt.Printf("\n⚠ provider reachable, but model %q was NOT in its model list (%d models).\n", info.Model, len(models))
			max := len(models)
			if max > 8 {
				max = 8
			}
			fmt.Printf("  available include: %s\n", strings.Join(models[:max], ", "))
		}
	}

	// Real round trip through the same code path explanations/chat use.
	fmt.Println("  sending a test request…")
	dur, err := tut.Ping(ctx)
	if err != nil {
		fmt.Printf("✗ chat request failed: %v\n", err)
		return nil
	}
	fmt.Printf("✓ chat round-trip OK in %s\n", dur.Round(time.Millisecond))
	return nil
}
