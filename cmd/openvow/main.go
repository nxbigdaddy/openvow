package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"openvow/internal/api"
	"openvow/internal/artifact"
	"openvow/internal/pipeline"
	"openvow/internal/store"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "terminal":
		cmdTerminal(os.Args[2:])
	case "task":
		cmdTask(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `openvow %s

usage:
  openvow serve    --db <path> --repo <path> [--artifacts <dir>] [--addr 127.0.0.1:7711]
  openvow terminal add --name <name> [--db path]
  openvow task new   --title <t> --writes <globs> --acceptance <json> [--kind implement]
  openvow task list  [--state queued]
  openvow version
`, version)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "openvow.db", "sqlite database file")
	repo := fs.String("repo", ".", "target repository root")
	artDir := fs.String("artifacts", "data/artifacts", "artifact store directory")
	addr := fs.String("addr", "127.0.0.1:7711", "listen address")
	lease := fs.Duration("lease", 30*time.Minute, "lease duration")
	maxFile := fs.Int64("max-file-bytes", 2<<20, "max single file size")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	art, err := artifact.New(*artDir)
	if err != nil {
		log.Fatalf("artifact store: %v", err)
	}

	pipe := &pipeline.Service{
		Store:         st,
		Artifacts:     art,
		Repo:          *repo,
		MaxFileBytes:  *maxFile,
		MaxTotalBytes: 50 << 20,
	}

	srv := api.New(st, art, pipe, api.Config{
		Repo:          *repo,
		MaxFileBytes:  *maxFile,
		MaxTotalBytes: 50 << 20,
		LeaseTTL:      *lease,
		Version:       version,
	})

	log.Printf("openvow %s listening on http://%s (db=%s repo=%s artifacts=%s)",
		version, *addr, *dbPath, *repo, *artDir)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
