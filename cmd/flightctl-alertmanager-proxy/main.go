// flightctl-alertmanager-proxy is a lightweight reverse proxy for Alertmanager that
// integrates with FlightControl's existing authentication and authorization system.
//
// It validates bearer tokens and authorizes requests before proxying them to
// Alertmanager running on localhost:9093. Users must have "get" access to the
// "alerts" resource to access the proxy.
//
// The proxy listens on port 8443 and requires:
// - Authorization: Bearer <token> header
//
// This works with all FlightControl auth types: OIDC, OpenShift, and AAP.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flightctl/flightctl/internal/auth"
	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/config"
	fclog "github.com/flightctl/flightctl/pkg/log"
	"github.com/sirupsen/logrus"
)

const (
	proxyPort       = ":8443"
	alertmanagerURL = "http://localhost:9093"
	alertsResource  = "alerts"
	getAction       = "get"
)

type AlertmanagerProxy struct {
	log    logrus.FieldLogger
	cfg    *config.Config
	proxy  *httputil.ReverseProxy
	target *url.URL
}

func NewAlertmanagerProxy(cfg *config.Config, log logrus.FieldLogger) (*AlertmanagerProxy, error) {
	target, err := url.Parse(alertmanagerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse alertmanager URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	return &AlertmanagerProxy{
		log:    log,
		cfg:    cfg,
		proxy:  proxy,
		target: target,
	}, nil
}

func (p *AlertmanagerProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract bearer token from Authorization header using FlightControl's utility
	token, err := common.ExtractBearerToken(r)
	if err != nil {
		p.log.WithError(err).Error("Failed to extract bearer token")
		http.Error(w, "Invalid Authorization header", http.StatusUnauthorized)
		return
	}

	// Validate token using FlightControl's auth system
	if err := auth.GetAuthN().ValidateToken(r.Context(), token); err != nil {
		p.log.WithError(err).Error("Token validation failed")
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// Create context with token for authorization check (using proper context key)
	ctx := context.WithValue(r.Context(), common.TokenCtxKey, token)

	// Check if user has permission to access alerts
	allowed, err := auth.GetAuthZ().CheckPermission(ctx, alertsResource, getAction)
	if err != nil {
		p.log.WithError(err).Error("Authorization check failed")
		http.Error(w, "Authorization service unavailable", http.StatusServiceUnavailable)
		return
	}

	if !allowed {
		p.log.Warn("User denied access to alerts")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	p.log.Infof("Proxying request to alertmanager, path: %s", r.URL.Path)

	// Proxy the request to Alertmanager
	p.proxy.ServeHTTP(w, r)
}

func main() {
	ctx := context.Background()

	// Initialize logging
	logger := fclog.InitLogs()
	logger.Println("Starting Alertmanager Proxy service")
	defer logger.Println("Alertmanager Proxy service stopped")

	// Load configuration
	cfg, err := config.LoadOrGenerate(config.ConfigFile())
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Set log level
	logLvl, err := logrus.ParseLevel(cfg.Service.LogLevel)
	if err != nil {
		logLvl = logrus.InfoLevel
	}
	logger.SetLevel(logLvl)

	// Initialize auth system
	if err := auth.InitAuth(cfg, logger); err != nil {
		logger.Fatalf("Failed to initialize auth: %v", err)
	}

	// Create proxy
	proxy, err := NewAlertmanagerProxy(cfg, logger)
	if err != nil {
		logger.Fatalf("Failed to create alertmanager proxy: %v", err)
	}

	// Create HTTP server
	server := &http.Server{
		Addr:    proxyPort,
		Handler: proxy,
	}

	// Handle graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	go func() {
		<-ctx.Done()
		logger.Println("Shutdown signal received")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Errorf("Server shutdown error: %v", err)
		}
	}()

	logger.Printf("Alertmanager proxy listening on %s, proxying to %s", proxyPort, alertmanagerURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("Server error: %v", err)
	}
}
