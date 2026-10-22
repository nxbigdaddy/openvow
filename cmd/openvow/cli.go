package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"

	"openvow/internal/store"
)

func openStore(dbPath string) *store.Store {
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	return st
}

func cmdTerminal(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: openvow terminal add --name <name> [--db path]")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("terminal add", flag.ExitOnError)
		name := fs.String("name", "", "terminal name")
		db := fs.String("db", "openvow.db", "sqlite database file")
		_ = fs.Parse(args[1:])
		if *name == "" {
			log.Fatal("--name is required")
		}
		st := openStore(*db)
		defer st.Close()
		id, token, err := st.CreateTerminal(*name)
		if err != nil {
			log.Fatalf("create terminal: %v", err)
		}
		fmt.Printf("id:    %s\n", id)
		fmt.Printf("token: %s\n", token)
		fmt.Println("\n令牌只显示这一次，请立刻保存。")
	default:
		fmt.Fprintf(os.Stderr, "unknown terminal command %q\n", args[0])
		os.Exit(2)
	}
}

func cmdTask(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: openvow task new|list ...")
		os.Exit(2)
	}
	switch args[0] {
	case "new":
		fs := flag.NewFlagSet("task new", flag.ExitOnError)
		db := fs.String("db", "openvow.db", "sqlite database file")
		kind := fs.String("kind", "implement", "interface|test|implement")
		title := fs.String("title", "", "task title")
		summary := fs.String("summary", "", "task summary")
		writes := fs.String("writes", "", "comma separated globs")
		acceptance := fs.String("acceptance", "", "acceptance JSON（短的一条可直接内联）")
		acceptanceFile := fs.String("acceptance-file", "", "从文件读取 acceptance JSON（推荐，避免 shell 引号问题）")
		requires := fs.String("requires", "", "comma separated task ids")
		maxAttempts := fs.Int("max-attempts", 2, "max attempts")
		_ = fs.Parse(args[1:])

		if *acceptanceFile != "" {
			b, err := os.ReadFile(*acceptanceFile)
			if err != nil {
				log.Fatalf("read acceptance file: %v", err)
			}
			*acceptance = string(b)
		}
		if *title == "" || *acceptance == "" {
			log.Fatal("--title and --acceptance are required")
		}
		if !json.Valid([]byte(*acceptance)) {
			log.Fatal("--acceptance is not valid JSON")
		}
		st := openStore(*db)
		defer st.Close()

		id := "T-" + randHex(4)
		tk := &store.Task{
			ID:          id,
			Kind:        *kind,
			Title:       *title,
			Summary:     *summary,
			Writes:      splitCSV(*writes),
			Requires:    splitCSV(*requires),
			Acceptance:  json.RawMessage(*acceptance),
			MaxAttempts: *maxAttempts,
		}
		if err := st.InsertTask(tk); err != nil {
			log.Fatalf("insert task: %v", err)
		}
		fmt.Println(id)

	case "list":
		fs := flag.NewFlagSet("task list", flag.ExitOnError)
		db := fs.String("db", "openvow.db", "sqlite database file")
		state := fs.String("state", "", "filter by state")
		_ = fs.Parse(args[1:])
		st := openStore(*db)
		defer st.Close()
		tasks, err := st.ListTasks(*state, 100)
		if err != nil {
			log.Fatalf("list: %v", err)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tKIND\tSTATE\tATTEMPTS\tTITLE")
		for _, t := range tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", t.ID, t.Kind, t.State, t.Attempts, t.Title)
		}
		w.Flush()

	default:
		fmt.Fprintf(os.Stderr, "unknown task command %q\n", args[0])
		os.Exit(2)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}
