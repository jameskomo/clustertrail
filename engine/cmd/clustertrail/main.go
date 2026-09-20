// clustertrail is the cluster engine behind the ClusterTrail desktop client.
// `serve` is what the shell runs; it binds loopback only.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"path/filepath"

	"github.com/jameskomo/clustertrail/engine/internal/api"
	"github.com/jameskomo/clustertrail/engine/internal/audit"
	"github.com/jameskomo/clustertrail/engine/internal/change"
	"github.com/jameskomo/clustertrail/engine/internal/cli"
	"github.com/jameskomo/clustertrail/engine/internal/cluster"
	"github.com/jameskomo/clustertrail/engine/internal/forward"
	"github.com/jameskomo/clustertrail/engine/internal/mcp"
	"github.com/jameskomo/clustertrail/engine/internal/store"
	"github.com/jameskomo/clustertrail/engine/internal/ui"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		// falls through to the server below
	case "drift":
		os.Exit(cli.Drift(os.Args[2:], os.Stdout, os.Stderr))
	case "mcp":
		os.Exit(mcp.Serve(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	default:
		usage()
	}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "loopback address to listen on; port 0 picks a free port")
	token := fs.String("token", os.Getenv("CLUSTERTRAIL_TOKEN"), "bearer token the UI must present (default: $CLUSTERTRAIL_TOKEN, else random)")
	kubeconfig := fs.String("kubeconfig", "", "explicit kubeconfig path (default: $KUBECONFIG or ~/.kube/config)")
	open := fs.Bool("open", false, "open the interface in your browser once the engine is listening")
	dataDir := fs.String("data", defaultDataDir(), "directory for the audit log and local state")
	_ = fs.Parse(os.Args[2:])

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *token == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		*token = hex.EncodeToString(b)
	}

	reg, err := cluster.Load(*kubeconfig)
	if err != nil {
		log.Error("startup", "err", err)
		os.Exit(1)
	}
	defer reg.Close()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	if !ln.Addr().(*net.TCPAddr).IP.IsLoopback() {
		log.Error("refusing to listen on a non-loopback address", "addr", ln.Addr())
		os.Exit(1)
	}

	auditLog, err := audit.Open(filepath.Join(*dataDir, "audit.jsonl"))
	if err != nil {
		log.Error("audit", "err", err)
		os.Exit(1)
	}
	snapshots, err := store.New(*dataDir)
	if err != nil {
		log.Error("snapshot cache", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: (&api.Server{
		Registry: reg, Gate: change.NewGate(reg, auditLog), Audit: auditLog, Forwards: forward.NewManager(), Snapshots: snapshots,
		Token: *token, Addr: ln.Addr().String(), Version: version, Log: log,
	}).Handler()}

	// The shell reads this line to learn where to connect.
	fmt.Printf("CLUSTERTRAIL addr=%s token=%s\n", ln.Addr(), *token)
	url := fmt.Sprintf("http://%s/?token=%s", ln.Addr(), *token)
	if ui.Built() {
		fmt.Printf("\n  ClusterTrail is running. Open:\n  %s\n\n", url)
		if *open {
			api.OpenBrowser(url)
		}
	} else if *open {
		log.Warn("this binary has no interface bundled in; build it with `make bundle`")
	}
	log.Info("serving", "addr", ln.Addr(), "contexts", len(reg.Clusters()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sd, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sd)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  clustertrail serve [--open] [--addr 127.0.0.1:0] [--token T] [--kubeconfig PATH] [--data DIR]")
	fmt.Fprintln(os.Stderr, "      run ClusterTrail: the engine, and the interface it serves")
	fmt.Fprintln(os.Stderr, "  clustertrail drift --repo DIR [--cluster CONTEXT] [--json]")
	fmt.Fprintln(os.Stderr, "      compare a repository with a cluster; exits 1 on drift")
	fmt.Fprintln(os.Stderr, "  clustertrail mcp [--kubeconfig PATH]")
	fmt.Fprintln(os.Stderr, "      read-only MCP server over stdio, for coding agents")
	os.Exit(2)
}

func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "clustertrail")
	}
	return ".clustertrail"
}
