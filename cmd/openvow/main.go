package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"openvow/internal/api"
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
  openvow serve   --db <path> --repo <path> [--addr 127.0.0.1:7711]
  openvow version
`, version)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "openvow.db", "sqlite database file")
	repo := fs.String("repo", ".", "target repository root")
	addr := fs.String("addr", "127.0.0.1:7711", "listen address")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := api.New(st, *repo, version)
	log.Printf("openvow %s listening on http://%s (db=%s repo=%s)", version, *addr, *dbPath, *repo)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
