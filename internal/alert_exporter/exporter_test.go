package alert_exporter

import (
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/flightctl/flightctl/api/v1alpha1"
)

func TestMetricsHandler_NoAlerts(t *testing.T) {
	activeAlerts.Store(AlertCheckpoint{Alerts: make(map[AlertKey]map[string]struct{})})

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	MetricsHandler(w, req)

	resp := w.Result()
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("unexpected Content-Type: %s", contentType)
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected no metrics, got: %s", w.Body.String())
	}
}

func TestMetricsHandler_SingleAlert(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {"DeviceCPUWarning": {}},
	}
	activeAlerts.Store(AlertCheckpoint{Alerts: alerts})

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	MetricsHandler(w, req)

	body := w.Body.String()
	expected := `fc_alert_active{org_id="org", resource_kind="Device", resource_name="dev1", reason="DeviceCPUWarning"} 1`
	if !strings.Contains(body, expected) {
		t.Errorf("expected metric not found, got: %s", body)
	}
}

func TestSetExclusiveAlert(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {
			"DeviceCPUWarning":   {},
			"DeviceDiskCritical": {},
			"DeviceDisconnected": {}, // should remain
		},
	}
	setExclusiveAlert(alerts, "org:Device:dev1", "DeviceCPUCritical", cpuGroup)
	setExclusiveAlert(alerts, "org:Device:dev1", "DeviceDiskWarning", diskGroup)

	reasons := alerts["org:Device:dev1"]
	if _, ok := reasons["DeviceCPUWarning"]; ok {
		t.Errorf("expected DeviceCPUWarning to be cleared")
	}
	if _, ok := reasons["DeviceCPUCritical"]; !ok {
		t.Errorf("expected DeviceCPUCritical to remain")
	}
	if _, ok := reasons["DeviceDiskCritical"]; ok {
		t.Errorf("expected DeviceDiskCritical to be cleared")
	}
	if _, ok := reasons["DeviceDiskWarning"]; !ok {
		t.Errorf("expected DeviceDiskWarning to remain")
	}
	if _, ok := reasons["DeviceDisconnected"]; !ok {
		t.Errorf("DeviceDisconnected should not be affected")
	}
}

func TestClearAlertGroup(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {
			"DeviceMemoryWarning":  {},
			"DeviceMemoryCritical": {},
		},
	}
	clearAlertGroup(alerts, "org:Device:dev1", memoryGroup)

	if _, exists := alerts["org:Device:dev1"]; exists {
		t.Errorf("expected key to be deleted after clearing all group reasons")
	}
}

func TestProcessEvent_AppStatus(t *testing.T) {
	alerts := make(map[AlertKey]map[string]struct{})
	ev := fakeEvent("org", "Device", "dev1", "DeviceApplicationError")
	processEvent(alerts, ev)

	reasons := alerts["org:Device:dev1"]
	if _, ok := reasons["DeviceApplicationError"]; !ok {
		t.Errorf("expected DeviceApplicationError to be set")
	}
}

func TestProcessEvent_AppHealthy(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {
			"DeviceApplicationError":    {},
			"DeviceApplicationDegraded": {},
		},
	}
	ev := fakeEvent("org", "Device", "dev1", "DeviceApplicationHealthy")
	processEvent(alerts, ev)

	if _, exists := alerts["org:Device:dev1"]; exists {
		t.Errorf("expected all application alerts to be cleared")
	}
}

func TestProcessEvent_Connected(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {"DeviceDisconnected": {}},
	}
	ev := fakeEvent("org", "Device", "dev1", "DeviceConnected")
	processEvent(alerts, ev)

	if _, exists := alerts["org:Device:dev1"]; exists {
		t.Errorf("expected DeviceDisconnected to be cleared and key removed")
	}
}

func TestProcessEvent_ResourceDeleted(t *testing.T) {
	alerts := map[AlertKey]map[string]struct{}{
		"org:Device:dev1": {
			"DeviceCPUWarning":   {},
			"DeviceDisconnected": {},
		},
	}
	ev := fakeEvent("org", "Device", "dev1", "ResourceDeleted")
	processEvent(alerts, ev)

	if _, exists := alerts["org:Device:dev1"]; exists {
		t.Errorf("expected all alerts to be cleared on ResourceDeleted")
	}
}

func fakeEvent(org, kind, name, reason string) api.Event {
	return api.Event{
		Reason: api.EventReason(reason),
		InvolvedObject: api.ObjectReference{
			Kind: kind,
			Name: name,
		},
	}
}
