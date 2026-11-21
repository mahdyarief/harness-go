package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"hsg/internal/models"
	"hsg/internal/repositories"
)

type options struct {
	dsn, queriesPath               string
	actor                          uuid.UUID
	scope                          models.ChatScope
	concurrency, iterations, limit int
	timeout, requestTimeout        time.Duration
}

func parseOptions(args []string) (options, error) {
	var o options
	var actor, tenant string
	fs := flag.NewFlagSet("kbbench", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Never echo arguments, which may contain credentials.
	fs.StringVar(&o.dsn, "dsn", "", "Required runtime PostgreSQL DSN; no environment fallback")
	fs.StringVar(&actor, "actor", "", "Required existing actor UUID")
	fs.StringVar(&tenant, "tenant", "", "Existing tenant UUID; omit for platform-global scope")
	fs.StringVar(&o.queriesPath, "queries", "", "Required JSON array of nonempty 1..500 character queries")
	fs.IntVar(&o.concurrency, "concurrency", 1, "Concurrent requests, 1..25")
	fs.IntVar(&o.iterations, "iterations", 1, "Full fixture repetitions, 1..10000, in fixed fixture order")
	fs.IntVar(&o.limit, "limit", 5, "Requested limit, 1..50; lexical baseline caps effective limit at 5")
	fs.DurationVar(&o.timeout, "timeout", 30*time.Second, "Whole-command deadline, at most 10m")
	fs.DurationVar(&o.requestTimeout, "request-timeout", 2*time.Second, "Per-request deadline, at most 30s and no longer than timeout")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return o, errors.New("invalid arguments")
	}
	if strings.TrimSpace(o.dsn) == "" || o.queriesPath == "" {
		return o, errors.New("dsn and queries flags are required")
	}
	var err error
	if o.actor, err = uuid.Parse(actor); err != nil || o.actor == uuid.Nil {
		return o, errors.New("actor must be a nonzero UUID")
	}
	o.scope.Kind = models.ChatPlatformPersonal
	if tenant != "" {
		o.scope.Kind = models.ChatTenantPersonal
		if o.scope.TenantID, err = uuid.Parse(tenant); err != nil || o.scope.TenantID == uuid.Nil {
			return o, errors.New("tenant must be a nonzero UUID")
		}
	}
	if o.concurrency < 1 || o.concurrency > 25 || o.iterations < 1 || o.iterations > 10000 || o.limit < 1 || o.limit > 50 {
		return o, errors.New("concurrency, iterations or limit out of range")
	}
	if o.timeout <= 0 || o.timeout > 10*time.Minute || o.requestTimeout <= 0 || o.requestTimeout > 30*time.Second || o.requestTimeout > o.timeout {
		return o, errors.New("timeout out of range")
	}
	return o, nil
}

func loadQueries(path string) ([]string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", errors.New("cannot open query fixture")
	}
	defer f.Close()
	const maxBytes = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(raw) > maxBytes {
		return nil, "", errors.New("cannot read query fixture or fixture exceeds 8 MiB")
	}
	var queries []string
	if !utf8.Valid(raw) || json.Unmarshal(raw, &queries) != nil || len(queries) == 0 {
		return nil, "", errors.New("fixture must be a nonempty JSON array of strings")
	}
	for _, query := range queries {
		if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 500 {
			return nil, "", errors.New("queries must contain 1..500 characters and not be blank")
		}
	}
	sum := sha256.Sum256(raw)
	return queries, hex.EncodeToString(sum[:]), nil
}

func execute(ctx context.Context, args []string, out io.Writer) int {
	fail := func(message string) int {
		_ = json.NewEncoder(out).Encode(struct {
			Error string `json:"error"`
		}{message})
		return 1
	}
	o, err := parseOptions(args)
	if err != nil {
		return fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	queries, hash, err := loadQueries(o.queriesPath)
	if err != nil {
		return fail(err.Error())
	}
	config, err := pgx.ParseConfig(o.dsn)
	if err != nil {
		return fail("invalid database configuration")
	}
	// Refuse remote hosts and fallbacks; do not resolve DNS or read staging data.
	ip := net.ParseIP(config.Host)
	if ip == nil || !ip.IsLoopback() || len(config.Fallbacks) != 0 {
		return fail("benchmark requires a literal loopback database host without fallbacks")
	}
	// All repository transactions are read-only, including on pooled connections.
	config.RuntimeParams["default_transaction_read_only"] = "on"
	config.RuntimeParams["row_security"] = "on"
	db := stdlib.OpenDB(*config)
	defer db.Close()
	db.SetMaxOpenConns(o.concurrency)
	db.SetMaxIdleConns(o.concurrency)
	setupCtx, setupCancel := context.WithTimeout(ctx, o.requestTimeout)
	defer setupCancel()
	unsafe, err := repositories.TenantRoleUnsafe(setupCtx, db)
	if err != nil || unsafe {
		return fail("database unavailable or runtime role/RLS configuration unsafe")
	}
	repo := repositories.NewKnowledgeRepository(db)
	effectiveLimit := min(o.limit, 5) // Existing Search has no stable candidate-limit contract.
	result := runBenchmark(ctx, queries, o.iterations, o.concurrency, o.requestTimeout, func(ctx context.Context, query string) (int, error) {
		hits, err := repo.Search(ctx, o.actor, o.scope, query, effectiveLimit)
		return len(hits), err
	})
	result.FixtureSHA256, result.Queries = hash, len(queries)
	result.Iterations, result.Concurrency = o.iterations, o.concurrency
	result.RequestedLimit, result.EffectiveLimit = o.limit, effectiveLimit
	result.Scope = o.scope.Kind
	if json.NewEncoder(out).Encode(result) != nil || result.Errors > 0 || result.Cancelled {
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout)
	stop()
	os.Exit(code)
}
