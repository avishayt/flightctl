// Package alert_exporter monitors recent events and maintains a live view of active alerts.
//
// Alert Structure:
//   Alerts are stored in-memory using the following structure:
//
//     map[AlertKey]map[string]struct{}
//
//   - AlertKey is a composite string in the format "org:kind:name", uniquely identifying a resource.
//   - The nested map tracks active alert reasons (as strings) for that resource.
//
// Alert Logic:
//   - Certain alert reasons are mutually exclusive and grouped (e.g., CPU status, application health).
//     Only one alert from a group may be active for a resource at a time.
//   - When a new event is processed:
//     - If it's part of an exclusive group (e.g., DeviceApplicationError), other group members are removed.
//     - If it's a "normal" or healthy event (e.g., DeviceCPUNormal), the entire group is cleared.
//     - DeviceDisconnected is added to the alert set, and DeviceConnected removes it.
//     - Terminal events (e.g., ResourceDeleted, DeviceDecommissioned) remove all alerts for the resource.
//
// Prometheus Integration:
//   - Active alerts are exposed via the /metrics endpoint as:
//
//       fc_alert_active{org_id="...", resource_kind="...", resource_name="...", reason="..."} 1
//
//   - When an alert is cleared (i.e., removed from the map), it will naturally expire from Prometheus
//     after the default staleness timeout (typically 5 minutes).
//
// Checkpointing:
//   - The exporter periodically saves its state (active alerts and the last processed event).
//   - On startup, it resumes from the last checkpoint to avoid reprocessing old events.

package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const AlertCheckpointConsumer = "alert-exporter"
const AlertCheckpointKey = "active-alerts"
const CurrentAlertCheckpointVersion = 1

type AlertKey string

type EventPoller struct {
	log      *logrus.Logger
	handler  service.Service
	interval time.Duration
}

type Alert struct {
	ResourceOrg  string
	ResourceKind string
	ResourceName string
	Reason       string
}

type AlertCheckpoint struct {
	Version   int
	LastEvent string
	Alerts    map[AlertKey]map[string]struct{}
}

var activeAlerts atomic.Value // stores AlertCheckpoint

func key(a Alert) AlertKey {
	return AlertKey(fmt.Sprintf("%s:%s:%s", a.ResourceOrg, a.ResourceKind, a.ResourceName))
}

func AlertFromKey(key AlertKey, reason string) Alert {
	return Alert{
		ResourceOrg:  strings.Split(string(key), ":")[0],
		ResourceKind: strings.Split(string(key), ":")[1],
		ResourceName: strings.Split(string(key), ":")[2],
		Reason:       reason,
	}
}

func NewEventPoller(log *logrus.Logger, handler service.Service, interval time.Duration) *EventPoller {
	return &EventPoller{
		log:      log,
		handler:  handler,
		interval: interval,
	}
}

func ResetActiveAlerts() {
	activeAlerts.Store(AlertCheckpoint{Version: CurrentAlertCheckpointVersion, Alerts: make(map[AlertKey]map[string]struct{})})
}

func (e *EventPoller) Poll(ctx context.Context) {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	params := e.GetListEventsParams()
	e.LoadCheckpoint(ctx)

	for {
		<-ticker.C
		tickerCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		e.ProcessLatestEvents(tickerCtx, params)
		cancel()
	}
}

// LoadCheckpoint retrieves the last processed event and active alerts from the database.
// If no checkpoint exists, it initializes a fresh state.
// If it fails to retrieve the checkpoint or unmarshal the contents, it logs an error and starts
// from a fresh state. This is better than panicking, as it allows the exporter to continue running
// and at least report new alerts from the point of failure onward.
// In the future, we could consider using a more robust error handling strategy, such as listing
// the system resources and reconstructing the list of active alerts based on the current state
// of the system. However, for now, I assume that if we fail to fetch the checkpoint then we will
// also fail to fetch the system resources.
func (e *EventPoller) LoadCheckpoint(ctx context.Context) {
	loadedCheckpoint := false
	previousCheckpoint, status := e.handler.GetCheckpoint(ctx, AlertCheckpointConsumer, AlertCheckpointKey)
	if status.Code != http.StatusOK {
		if status.Code == http.StatusNotFound {
			e.log.Info("No alert checkpoint found")
		} else {
			e.log.Errorf("Failed to get alert checkpoint: %v", status.Message)
		}
	}

	if status.Code == http.StatusOK && previousCheckpoint != nil {
		var checkpoint AlertCheckpoint
		if err := json.Unmarshal(previousCheckpoint, &checkpoint); err != nil {
			e.log.Errorf("Failed to unmarshal alert checkpoint: %v", err)
		} else {
			activeAlerts.Store(checkpoint)
			loadedCheckpoint = true
			e.log.Infof("Resuming from last event: %s", checkpoint.LastEvent)
		}
	}
	if !loadedCheckpoint {
		activeAlerts.Store(AlertCheckpoint{Version: CurrentAlertCheckpointVersion, Alerts: make(map[AlertKey]map[string]struct{})})
		e.log.Info("Starting with a fresh state")
	}
}

func (e *EventPoller) GetListEventsParams() api.ListEventsParams {
	eventsOfInterest := []api.EventReason{
		api.DeviceApplicationDegraded,
		api.DeviceApplicationError,
		api.DeviceApplicationHealthy,
		api.DeviceCPUCritical,
		api.DeviceCPUNormal,
		api.DeviceCPUWarning,
		api.DeviceConnected,
		api.DeviceDisconnected,
		api.DeviceMemoryCritical,
		api.DeviceMemoryNormal,
		api.DeviceMemoryWarning,
		api.DeviceDiskCritical,
		api.DeviceDiskNormal,
		api.DeviceDiskWarning,
		api.ResourceDeleted,
		api.DeviceDecommissioned,
	}
	return api.ListEventsParams{
		Order: lo.ToPtr(api.Asc),
		FieldSelector: lo.ToPtr(fmt.Sprintf(
			"reason in (%s)",
			strings.Join(lo.Map(eventsOfInterest, func(r api.EventReason, _ int) string { return string(r) }), ","))),
		Limit: lo.ToPtr(int32(1000)),
	}
}

func (e *EventPoller) ProcessLatestEvents(ctx context.Context, params api.ListEventsParams) {
	lastEvent := ""
	oldCheckpoint, _ := activeAlerts.Load().(AlertCheckpoint)
	needToDiscardedFirstEvent := false
	if oldCheckpoint.LastEvent != "" {
		params.Continue = lo.ToPtr(*store.BuildContinueString(oldCheckpoint.LastEvent, 0))
		// We used the last processed event in the Continue parameter rather than
		// the next event (because the next event wasn't known yet). Therefore, we
		// discard the first event to avoid processing it twice.
		needToDiscardedFirstEvent = true
	}

	for {
		// List the events since the last checkpoint
		events, status := e.handler.ListEvents(ctx, params)
		if status.Code != http.StatusOK {
			log.Printf("Failed to list events: %v", status)
			break
		}

		for _, ev := range events.Items {
			if needToDiscardedFirstEvent {
				needToDiscardedFirstEvent = false
				continue
			}
			lastEvent = (*ev.Metadata.CreationTimestamp).Format(time.RFC3339)
			processEvent(oldCheckpoint.Alerts, ev)
		}

		if events.Metadata.Continue == nil {
			break // No more events to process
		}
		params.Continue = events.Metadata.Continue
	}

	checkpoint := AlertCheckpoint{Version: CurrentAlertCheckpointVersion, Alerts: oldCheckpoint.Alerts, LastEvent: lastEvent}
	checkpointData, err := json.Marshal(checkpoint)
	if err != nil {
		e.log.Fatalf("Failed to marshal alert checkpoint: %v", err)
	}
	activeAlerts.Store(checkpoint)
	e.handler.SetCheckpoint(ctx, AlertCheckpointConsumer, AlertCheckpointKey, checkpointData)
}

var (
	appStatusGroup = []string{string(api.DeviceApplicationError), string(api.DeviceApplicationDegraded)}
	cpuGroup       = []string{string(api.DeviceCPUCritical), string(api.DeviceCPUWarning)}
	memoryGroup    = []string{string(api.DeviceMemoryCritical), string(api.DeviceMemoryWarning)}
	diskGroup      = []string{string(api.DeviceDiskCritical), string(api.DeviceDiskWarning)}
)

func processEvent(alerts map[AlertKey]map[string]struct{}, event api.Event) {
	alert := Alert{
		ResourceOrg:  store.NullOrgId.String(),
		ResourceKind: event.InvolvedObject.Kind,
		ResourceName: event.InvolvedObject.Name,
		Reason:       string(event.Reason),
	}
	k := key(alert)

	switch event.Reason {
	case api.ResourceDeleted, api.DeviceDecommissioned:
		delete(alerts, k)
	// Applications
	case api.DeviceApplicationError:
		setExclusiveAlert(alerts, k, string(api.DeviceApplicationError), appStatusGroup)
	case api.DeviceApplicationDegraded:
		setExclusiveAlert(alerts, k, string(api.DeviceApplicationDegraded), appStatusGroup)
	case api.DeviceApplicationHealthy:
		clearAlertGroup(alerts, k, appStatusGroup)
	// CPU
	case api.DeviceCPUCritical:
		setExclusiveAlert(alerts, k, string(api.DeviceCPUCritical), cpuGroup)
	case api.DeviceCPUWarning:
		setExclusiveAlert(alerts, k, string(api.DeviceCPUWarning), cpuGroup)
	case api.DeviceCPUNormal:
		clearAlertGroup(alerts, k, cpuGroup)
	// Memory
	case api.DeviceMemoryCritical:
		setExclusiveAlert(alerts, k, string(api.DeviceMemoryCritical), memoryGroup)
	case api.DeviceMemoryWarning:
		setExclusiveAlert(alerts, k, string(api.DeviceMemoryWarning), memoryGroup)
	case api.DeviceMemoryNormal:
		clearAlertGroup(alerts, k, memoryGroup)
	// Disk
	case api.DeviceDiskCritical:
		setExclusiveAlert(alerts, k, string(api.DeviceDiskCritical), diskGroup)
	case api.DeviceDiskWarning:
		setExclusiveAlert(alerts, k, string(api.DeviceDiskWarning), diskGroup)
	case api.DeviceDiskNormal:
		clearAlertGroup(alerts, k, diskGroup)
	// Device connection status
	case api.DeviceDisconnected:
		if _, exists := alerts[k]; !exists {
			alerts[k] = make(map[string]struct{})
		}
		alerts[k][string(api.DeviceDisconnected)] = struct{}{}
	case api.DeviceConnected:
		if reasons, exists := alerts[k]; exists {
			delete(reasons, string(api.DeviceDisconnected))
			if len(reasons) == 0 {
				delete(alerts, k)
			}
		}
	}
}

func setExclusiveAlert(alerts map[AlertKey]map[string]struct{}, key AlertKey, reason string, group []string) {
	if _, exists := alerts[key]; !exists {
		alerts[key] = make(map[string]struct{})
	}
	for _, r := range group {
		delete(alerts[key], r)
	}
	alerts[key][reason] = struct{}{}
}

func clearAlertGroup(alerts map[AlertKey]map[string]struct{}, key AlertKey, group []string) {
	if reasons, exists := alerts[key]; exists {
		for _, r := range group {
			delete(reasons, r)
		}
		if len(reasons) == 0 {
			delete(alerts, key)
		}
	}
}

func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	alerts := activeAlerts.Load()
	if alerts == nil {
		return
	}

	data := alerts.(AlertCheckpoint)
	for key, reasons := range data.Alerts {
		for reason := range reasons {
			alert := AlertFromKey(key, reason)
			fmt.Fprintf(w, "fc_alert_active{org_id=\"%s\", resource_kind=\"%s\", resource_name=\"%s\", reason=\"%s\"} 1\n",
				alert.ResourceOrg, alert.ResourceKind, alert.ResourceName, alert.Reason)
		}
	}
}
