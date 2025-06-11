package alert_exporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

type AlertSender struct {
	log      *logrus.Logger
	hostname string
	port     uint
}

func NewAlertSender(log *logrus.Logger, hostname string, port uint) *AlertSender {
	return &AlertSender{
		log:      log,
		hostname: hostname,
		port:     port,
	}
}

func (a *AlertSender) SendAlerts(checkpoint *AlertCheckpoint) error {
	err := a.sendAlerts(checkpoint.Updated)
	if err != nil {
		return err
	}

	a.cleanupAlerts(checkpoint)
	return nil
}

type AlertmanagerAlert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
}

// Send a list of alerts in batches to Alertmanager
func (a *AlertSender) sendAlerts(alerts []*AlertInfo) error {
	const batchSize = 100
	alertmanagerAlerts := make([]AlertmanagerAlert, 0, len(alerts))

	for _, alert := range alerts {
		// Construct the AlertmanagerAlert from your AlertInfo
		alertmanagerAlert := AlertmanagerAlert{
			Labels: map[string]string{
				"alertname": alert.Reason,
				"resource":  alert.ResourceName,
			},
			StartsAt: alert.StartsAt,
		}
		if alert.EndsAt != nil {
			alertmanagerAlert.EndsAt = *alert.EndsAt
		}

		alertmanagerAlerts = append(alertmanagerAlerts, alertmanagerAlert)

		// Send the batch if it's full
		if len(alertmanagerAlerts) >= batchSize {
			err := a.postBatch(alertmanagerAlerts)
			if err != nil {
				return fmt.Errorf("failed to send alerts: %v", err)
			}
			alertmanagerAlerts = alertmanagerAlerts[:0] // reset
		}
	}

	// Send any remaining alerts
	if len(alertmanagerAlerts) > 0 {
		err := a.postBatch(alertmanagerAlerts)
		if err != nil {
			return fmt.Errorf("failed to send alerts: %v", err)
		}
	}

	return nil
}

// Helper function to post a batch of alerts
func (a *AlertSender) postBatch(batch []AlertmanagerAlert) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal alerts: %v", err)
	}

	req, err := http.NewRequest("POST", a.hostname+":"+strconv.Itoa(int(a.port))+"/api/v1/alerts", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send alerts: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("alertmanager returned status %s", resp.Status)
	}
	return nil
}

// Remove alerts that have been resolved (endedAt != nil)
func (a *AlertSender) cleanupAlerts(checkpoint *AlertCheckpoint) {
	for i, alerts := range checkpoint.Alerts {
		for _, alert := range alerts {
			if alert.EndsAt != nil {
				delete(alerts, alert.Reason)
			}
		}
		if len(alerts) == 0 {
			delete(checkpoint.Alerts, i)
		}
	}
}
