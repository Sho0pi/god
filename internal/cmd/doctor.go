package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sho0pi/god/internal/config"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check health of all god dependencies",
	// Load config best-effort: doctor must still work if the config is broken or
	// missing, but when it parses we want checks (exec, postgres, whatsapp) to see it.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config")
		a := &app{cfgFile: path}
		if l, err := config.Load(path); err == nil {
			a.loader = l
			a.cfg = l.Cfg
		} else {
			fmt.Printf("  !  config not loaded (%v) — running checks with defaults\n\n", err)
		}
		withApp(cmd, a)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := appFrom(cmd).cfg
		checks := []check{
			checkGeminiKey(),
			checkDDGSearch(),
			checkDocker(),
			checkPostgres(cfg),
			checkWhatsAppSession(cfg),
		}

		allOK := true
		for _, c := range checks {
			if c.ok {
				fmt.Printf("  ✓  %s\n", c.name)
			} else {
				fmt.Printf("  ✗  %s\n     %s\n", c.name, c.hint)
				allOK = false
			}
		}

		fmt.Println()
		if allOK {
			fmt.Println("All checks passed.")
		} else {
			fmt.Println("Some checks failed — fix the issues above and re-run `god doctor`.")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

type check struct {
	name string
	ok   bool
	hint string
}

func pass(name string) check { return check{name: name, ok: true} }
func fail(name, hint string) check {
	return check{name: name, ok: false, hint: hint}
}

func checkGeminiKey() check {
	name := "GEMINI_API_KEY"
	if os.Getenv("GEMINI_API_KEY") != "" {
		return pass(name + " is set")
	}
	return fail(name+" is missing", "export GEMINI_API_KEY=<your-key>  (get one at aistudio.google.com)")
}

func checkDDGSearch() check {
	name := "ddg-search binary"
	path, err := exec.LookPath("ddg-search")
	if err != nil {
		return fail(name+" not found",
			"run: go install github.com/Djarvur/ddg-search/cmd/ddg-search@latest")
	}
	// Quick smoke test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--max-results", "1", "test").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return fail(name+" installed but not responding",
			"try running: ddg-search 'hello world'")
	}
	return pass(name + " installed and working")
}

func checkDocker() check {
	name := "Docker"
	_, err := exec.LookPath("docker")
	if err != nil {
		return fail(name+" not found", "install Docker: https://docs.docker.com/get-docker/")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return fail(name+" daemon not running", "run: open -a Docker  (or: sudo systemctl start docker)")
	}
	return pass(fmt.Sprintf("%s daemon running (v%s)", name, strings.TrimSpace(string(out))))
}

func checkPostgres(cfg *config.Config) check {
	name := "PostgreSQL (pgvector)"

	url := os.Getenv("DATABASE_URL")
	if url == "" && cfg != nil {
		url = cfg.Database.URL
	}
	if url == "" {
		return fail(name+" — no DATABASE_URL",
			"set DATABASE_URL or add database.url to god.yaml  (or run: docker-compose up -d)")
	}

	// Parse host:port from postgres URL (postgres://user:pass@host:port/db)
	host, port := "localhost", "5432"
	trimmed := strings.TrimPrefix(url, "postgres://")
	trimmed = strings.TrimPrefix(trimmed, "postgresql://")
	if idx := strings.Index(trimmed, "@"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if strings.Contains(trimmed, ":") {
		parts := strings.SplitN(trimmed, ":", 2)
		host, port = parts[0], parts[1]
	} else if trimmed != "" {
		host = trimmed
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 3*time.Second)
	if err != nil {
		return fail(
			fmt.Sprintf("%s not reachable at %s:%s", name, host, port),
			"run: docker-compose up -d",
		)
	}
	_ = conn.Close()
	return pass(fmt.Sprintf("%s reachable at %s:%s", name, host, port))
}

func checkWhatsAppSession(cfg *config.Config) check {
	name := "WhatsApp session"

	storePath := "data/whatsapp"
	if cfg != nil && cfg.Connectors.WhatsApp.StorePath != "" {
		storePath = cfg.Connectors.WhatsApp.StorePath
	}

	entries, err := os.ReadDir(storePath)
	if err != nil || len(entries) == 0 {
		return fail(name+" — no session found",
			fmt.Sprintf("run `god whatsapp` and scan the QR code (session stored in %s/)", storePath))
	}
	return pass(fmt.Sprintf("%s found in %s/", name, storePath))
}
