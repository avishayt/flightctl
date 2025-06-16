package alert_exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flightctl/flightctl/internal/config"
	"github.com/sirupsen/logrus"
)

const SentinelAlertName = "__sentinel_alert__"
const batchSize = 100

// Default retry configuration constants (used as fallbacks)
const (
	defaultMaxRetries     = 3
	defaultBaseDelay      = 500 * time.Millisecond
	defaultMaxDelay       = 10 * time.Second
	defaultRequestTimeout = 10 * time.Second
)

type AlertmanagerClient struct {
	hostname       string
	port           uint
	log            logrus.FieldLogger
	maxRetries     int
	baseDelay      time.Duration
	maxDelay       time.Duration
	requestTimeout time.Duration
}

type AlertmanagerAlert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

func NewAlertmanagerClient(hostname string, port uint, log logrus.FieldLogger, cfg *config.Config) *AlertmanagerClient {
	client := &AlertmanagerClient{
		hostname:       hostname,
		port:           port,
		log:            log,
		maxRetries:     3, // Default values
		baseDelay:      500 * time.Millisecond,
		maxDelay:       10 * time.Second,
		requestTimeout: 10 * time.Second,
	}

	// Apply configuration if provided
	if cfg != nil && cfg.Alertmanager != nil {
		if cfg.Alertmanager.MaxRetries > 0 && cfg.Alertmanager.MaxRetries <= 10 {
			client.maxRetries = cfg.Alertmanager.MaxRetries
		}

		if cfg.Alertmanager.BaseDelay != "" {
			if baseDelay, err := time.ParseDuration(cfg.Alertmanager.BaseDelay); err == nil && baseDelay > 0 {
				client.baseDelay = baseDelay
			} else {
				log.WithFields(logrus.Fields{
					"configured_base_delay": cfg.Alertmanager.BaseDelay,
					"error":                 err,
				}).Warn("Invalid base delay configuration, using default 500ms")
			}
		}

		if cfg.Alertmanager.MaxDelay != "" {
			if maxDelay, err := time.ParseDuration(cfg.Alertmanager.MaxDelay); err == nil && maxDelay > client.baseDelay {
				client.maxDelay = maxDelay
			} else {
				log.WithFields(logrus.Fields{
					"configured_max_delay": cfg.Alertmanager.MaxDelay,
					"error":                err,
				}).Warn("Invalid max delay configuration, using default 10s")
			}
		}
	}

	log.WithFields(logrus.Fields{
		"hostname":        client.hostname,
		"port":            client.port,
		"max_retries":     client.maxRetries,
		"base_delay":      client.baseDelay,
		"max_delay":       client.maxDelay,
		"request_timeout": client.requestTimeout,
	}).Info("Alertmanager client initialized")

	return client
}

// SendAllAlerts sends all alerts from a nested map to Alertmanager in batches.
func (a *AlertmanagerClient) SendAllAlerts(alerts map[AlertKey]map[string]*AlertInfo) error {
	alertBatch := make([]AlertmanagerAlert, 0, len(alerts))

	for _, alerts := range alerts {
		for _, alert := range alerts {
			alertBatch = append(alertBatch, alertToAlertmanagerAlert(alert))

			// Send the batch if it's full
			if len(alertBatch) >= batchSize {
				err := a.postBatchWithRetry(alertBatch)
				if err != nil {
					return fmt.Errorf("error sending alerts: %v", err)
				}
				alertBatch = alertBatch[:0] // reset
			}
		}
	}

	// Send any remaining alerts
	if len(alertBatch) > 0 {
		err := a.postBatchWithRetry(alertBatch)
		if err != nil {
			return fmt.Errorf("error sending alerts: %v", err)
		}
	}

	return nil
}

// postBatchWithRetry posts a batch of alerts with exponential backoff retry logic
func (a *AlertmanagerClient) postBatchWithRetry(batch []AlertmanagerAlert) error {
	var lastErr error

	logger := a.log.WithFields(logrus.Fields{
		"component":   "alertmanager_client",
		"alert_count": len(batch),
		"max_retries": a.maxRetries,
		"base_delay":  a.baseDelay,
		"max_delay":   a.maxDelay,
	})

	logger.Debug("Starting alert batch send with retry")

	for attempt := 0; attempt < a.maxRetries; attempt++ {
		attemptLogger := logger.WithFields(logrus.Fields{
			"attempt":      attempt + 1,
			"max_attempts": a.maxRetries,
		})

		attemptLogger.Debug("Attempting to send alert batch")

		err := a.postBatch(batch)
		if err == nil {
			if attempt > 0 {
				attemptLogger.WithFields(logrus.Fields{
					"success_on_attempt": attempt + 1,
					"total_attempts":     attempt + 1,
				}).Infof("Successfully sent alert batch after %d retries", attempt)
			} else {
				attemptLogger.Debug("Successfully sent alert batch on first attempt")
			}
			return nil
		}

		lastErr = err

		// Check if the error is retryable
		if !isRetryableError(err) {
			attemptLogger.WithFields(logrus.Fields{
				"error":      err,
				"error_type": fmt.Sprintf("%T", err),
				"retryable":  false,
			}).Error("Non-retryable error sending alerts")
			return err
		}

		// Calculate backoff delay if we haven't reached max retries
		if attempt < a.maxRetries-1 {
			delay := a.calculateBackoff(attempt)
			AlertmanagerRetriesTotal.Inc()
			attemptLogger.WithFields(logrus.Fields{
				"error":         err,
				"error_type":    fmt.Sprintf("%T", err),
				"retryable":     true,
				"backoff_delay": delay,
				"next_attempt":  attempt + 2,
			}).Warn("Failed to send alert batch, will retry")
			time.Sleep(delay)
		}
	}

	logger.WithFields(logrus.Fields{
		"final_error":   lastErr,
		"error_type":    fmt.Sprintf("%T", lastErr),
		"attempts_made": a.maxRetries,
	}).Error("Failed to send alert batch after all retry attempts")

	return fmt.Errorf("error sending alerts after %d attempts: %v", a.maxRetries, lastErr)
}

// Helper function to post a batch of alerts
func (a *AlertmanagerClient) postBatch(batch []AlertmanagerAlert) error {
	startTime := time.Now()
	defer func() {
		AlertmanagerRequestDurationSeconds.Observe(time.Since(startTime).Seconds())
	}()

	body, err := json.Marshal(batch)
	if err != nil {
		AlertmanagerRequestsTotal.WithLabelValues("marshal_error").Inc()
		return fmt.Errorf("failed to marshal alerts: %v", err)
	}

	url := fmt.Sprintf("http://%s:%d/api/v2/alerts", a.hostname, a.port)

	ctx, cancel := context.WithTimeout(context.Background(), a.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		AlertmanagerRequestsTotal.WithLabelValues("request_error").Inc()
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: a.requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		AlertmanagerRequestsTotal.WithLabelValues("network_error").Inc()
		return fmt.Errorf("failed to send alerts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		AlertmanagerRequestsTotal.WithLabelValues("http_error").Inc()
		return fmt.Errorf("alertmanager returned status %s", resp.Status)
	}

	AlertmanagerRequestsTotal.WithLabelValues("success").Inc()
	return nil
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors
	if netErr, ok := err.(*net.OpError); ok {
		return netErr.Temporary() || netErr.Timeout()
	}

	// Check for DNS errors
	if dnsErr, ok := err.(*net.DNSError); ok {
		return dnsErr.Temporary()
	}

	// Check for timeout errors
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}

	// Check for specific HTTP status codes that are retryable
	errStr := err.Error()
	retryableStatuses := []string{
		"500", "502", "503", "504", // Server errors
		"408", "429", // Request timeout, too many requests
	}

	for _, status := range retryableStatuses {
		if strings.Contains(errStr, status) {
			return true
		}
	}

	// Retry on connection refused (alertmanager might be starting up)
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
		return true
	}

	return false
}

// calculateBackoff calculates the delay for exponential backoff
func (a *AlertmanagerClient) calculateBackoff(attempt int) time.Duration {
	delay := a.baseDelay * time.Duration(1<<uint(attempt)) // 2^attempt
	if delay > a.maxDelay {
		delay = a.maxDelay
	}
	return delay
}

func alertToAlertmanagerAlert(alert *AlertInfo) AlertmanagerAlert {
	alertmanagerAlert := AlertmanagerAlert{
		Labels: map[string]string{
			"alertname": alert.Reason,
			"resource":  alert.ResourceName,
			"org_id":    alert.OrgID,
		},
		StartsAt: alert.StartsAt,
	}
	if alert.EndsAt != nil {
		alertmanagerAlert.EndsAt = *alert.EndsAt
	}
	return alertmanagerAlert
}
