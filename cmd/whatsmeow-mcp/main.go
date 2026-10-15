// Command whatsmeow-mcp is a WhatsApp MCP server in a single binary: it holds
// a linked-device session via whatsmeow and serves MCP over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/mcpserver"
	"github.com/Kieldro/whatsmeow-mcp/internal/rest"
	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

var version = "0.1.0"

func defaultDataDir() string {
	if d := os.Getenv("WHATSMEOW_MCP_DATA_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "whatsmeow-mcp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "whatsmeow-mcp-data"
	}
	return filepath.Join(home, ".local", "share", "whatsmeow-mcp")
}

func usage() {
	fmt.Fprintf(os.Stderr, `whatsmeow-mcp %s - WhatsApp MCP server (stdio) backed by whatsmeow

Usage:
  whatsmeow-mcp [flags]          serve MCP over stdio (default)
  whatsmeow-mcp login [flags]    pair this device (QR code or --pair-phone), then exit
  whatsmeow-mcp version

Flags:
`, version)
	flag.PrintDefaults()
}

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for the session and message databases (env WHATSMEOW_MCP_DATA_DIR)")
	restAddr := flag.String("rest", "", "also serve the REST API on this address, e.g. :8080 (a bare :port binds to 127.0.0.1)")
	pairPhone := flag.String("pair-phone", "", "pair with a phone-number code instead of a QR code (digits with country code)")
	noConnect := flag.Bool("no-connect", false, "offline mode: open the local databases but never connect to WhatsApp")
	noMCP := flag.Bool("no-mcp", false, "do not serve MCP on stdio (daemon mode, use with --rest)")
	logLevel := flag.String("log-level", "INFO", "log level for stderr: DEBUG, INFO, WARN, ERROR")
	flag.Usage = usage

	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	_ = flag.CommandLine.Parse(args)
	if cmd == "serve" && flag.NArg() > 0 {
		// Allow flags before the subcommand: whatsmeow-mcp --data-dir X login
		cmd = flag.Arg(0)
		_ = flag.CommandLine.Parse(flag.Args()[1:])
	}
	*pairPhone = jidutil.DigitsOnly(*pairPhone)

	switch cmd {
	case "version":
		fmt.Println(version)
		return
	case "serve", "login":
	default:
		usage()
		os.Exit(2)
	}

	// stdout is the MCP channel: all logs go to stderr.
	logger := newLogger(os.Stderr, "whatsmeow-mcp", *logLevel)
	if err := run(cmd, *dataDir, *restAddr, *pairPhone, *noConnect, *noMCP, logger); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd, dataDir, restAddr, pairPhone string, offline, noMCP bool, logger waLog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	// One process per session: a second client on the same device would fight
	// over the connection.
	unlock, err := lockDir(dataDir)
	if err != nil {
		return fmt.Errorf("%w (another whatsmeow-mcp is using %s; use its --rest API or a different --data-dir)", err, dataDir)
	}
	defer unlock()

	svc, err := wa.New(ctx, wa.Options{DataDir: dataDir, Log: logger, Offline: offline})
	if err != nil {
		return err
	}
	defer svc.Close()

	if cmd == "login" {
		if offline {
			return errors.New("login cannot run with --no-connect")
		}
		if svc.LoggedIn() {
			fmt.Fprintln(os.Stderr, "Already paired. Delete", filepath.Join(dataDir, "whatsapp.db"), "to pair again.")
			return nil
		}
		lctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := svc.Login(lctx, pairPhone, os.Stderr); err != nil {
			return err
		}
		// Give the phone a moment to push the initial history sync.
		fmt.Fprintln(os.Stderr, "Waiting 20s for the initial sync...")
		select {
		case <-time.After(20 * time.Second):
		case <-ctx.Done():
		}
		return nil
	}

	if !offline {
		if svc.LoggedIn() {
			if err := svc.Connect(ctx); err != nil {
				logger.Errorf("connect failed: %v (tools that need WhatsApp will report not connected)", err)
			}
		} else {
			logger.Warnf("This device is not paired. Run `whatsmeow-mcp login` in a terminal; trying QR login on stderr meanwhile.")
			go func() {
				if err := svc.Login(ctx, pairPhone, os.Stderr); err != nil {
					logger.Errorf("login: %v", err)
				}
			}()
		}
	}

	if restAddr != "" {
		if strings.HasPrefix(restAddr, ":") {
			restAddr = "127.0.0.1" + restAddr
		}
		srv := &http.Server{Addr: restAddr, Handler: rest.Handler(svc), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			logger.Infof("REST API listening on http://%s", restAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Errorf("REST server: %v", err)
			}
		}()
		defer srv.Close()
	}

	if noMCP {
		<-ctx.Done()
		return nil
	}
	server := mcpserver.New(svc, version)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		if strings.Contains(err.Error(), "EOF") {
			return nil // client closed stdin
		}
		return err
	}
	return nil
}
