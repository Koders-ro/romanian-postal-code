// Command server runs the Romanian postal code API.
//
// Environment:
//
//	API_KEY      required; one key, or several separated by commas (for rotation)
//	PORT         listen port (default 8080)
//	MAX_BATCH    max items per /v1/verify/batch request (default 500)
//	RATE_LIMIT   requests per minute per client IP on /v1/* (default 0 = off)
//	TRUST_PROXY  "true" to read the client IP from X-Forwarded-For (behind Coolify/Traefik, nginx...)
//	LOG_LEVEL    debug | info | warn | error (default info)
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rocode/data"
	"rocode/internal/api"
	"rocode/internal/store"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz on the local server and exit (for Docker HEALTHCHECK)")
	flag.Parse()

	if *healthcheck {
		os.Exit(probe(env("PORT", "8080")))
	}
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, port, err := config()
	if err != nil {
		return err
	}

	start := time.Now()
	st, err := store.Load(bytes.NewReader(data.Postal))
	if err != nil {
		return err
	}
	slog.Info("dataset loaded", "rows", st.Rows, "codes", st.Codes, "localities", st.Localities,
		"streets", st.Streets, "duration_ms", time.Since(start).Milliseconds())

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.New(st, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "port", port, "version", version,
			"rate_limit", cfg.RateLimit, "trust_proxy", cfg.TrustProxy, "api_keys", len(cfg.APIKeys))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func config() (api.Config, string, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return api.Config{}, "", fmt.Errorf("LOG_LEVEL: %w", err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	cfg := api.Config{Version: version}
	for _, k := range strings.Split(os.Getenv("API_KEY"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			cfg.APIKeys = append(cfg.APIKeys, k)
			if len(k) < 16 {
				slog.Warn("an API key is shorter than 16 characters; use a long random key (e.g. openssl rand -hex 32)")
			}
		}
	}
	if len(cfg.APIKeys) == 0 {
		return cfg, "", errors.New("API_KEY environment variable is required")
	}

	port := env("PORT", "8080")
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return cfg, "", fmt.Errorf("PORT: invalid value %q", port)
	}
	var err error
	if cfg.MaxBatch, err = intEnv("MAX_BATCH", 500); err != nil {
		return cfg, "", err
	}
	if cfg.RateLimit, err = intEnv("RATE_LIMIT", 0); err != nil {
		return cfg, "", err
	}
	if cfg.TrustProxy, err = strconv.ParseBool(env("TRUST_PROXY", "false")); err != nil {
		return cfg, "", fmt.Errorf("TRUST_PROXY: %w", err)
	}
	return cfg, port, nil
}

func probe(port string) int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func intEnv(k string, def int) (int, error) {
	n, err := strconv.Atoi(env(k, strconv.Itoa(def)))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: must be a non-negative integer", k)
	}
	return n, nil
}
