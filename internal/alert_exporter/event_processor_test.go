package alert_exporter

import (
	"fmt"
	"testing"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/samber/lo"
)

func TestSetExclusiveAlert(t *testing.T) {
	now := time.Now()
	event1 := fakeEvent("org", "Device", "dev1", string(api.DeviceCPUCritical))
	event2 := fakeEvent("org", "Device", "dev1", string(api.DeviceDiskWarning))
	alerts := map[AlertKey]map[string]*AlertInfo{
		AlertKeyFromEvent(event1): {
			string(api.DeviceCPUWarning):   &AlertInfo{StartsAt: now},
			string(api.DeviceDiskCritical): &AlertInfo{StartsAt: now},
			string(api.DeviceDisconnected): &AlertInfo{StartsAt: now}, // should remain
		},
	}
	checkpointCtx := &CheckpointContext{
		alerts:        alerts,
		updatedAlerts: []*AlertInfo{},
	}

	checkpointCtx.setAlert(event1, string(api.DeviceCPUCritical), cpuGroup)
	checkpointCtx.setAlert(event2, string(api.DeviceDiskWarning), diskGroup)

	reasons := alerts[AlertKeyFromEvent(event1)]
	if reasons[string(api.DeviceCPUWarning)] == nil || reasons[string(api.DeviceCPUWarning)].EndsAt == nil {
		fmt.Printf("reasons: %v\n", reasons[string(api.DeviceCPUWarning)])
		t.Errorf("expected DeviceCPUWarning to be resolved")
	}
	if reasons[string(api.DeviceCPUCritical)] == nil || reasons[string(api.DeviceCPUCritical)].EndsAt != nil {
		t.Errorf("expected DeviceCPUCritical to be active")
	}
	if reasons[string(api.DeviceDiskCritical)] == nil || reasons[string(api.DeviceDiskCritical)].EndsAt == nil {
		t.Errorf("expected DeviceDiskCritical to be resolved")
	}
	if reasons[string(api.DeviceDiskWarning)] == nil || reasons[string(api.DeviceDiskWarning)].EndsAt != nil {
		t.Errorf("expected DeviceDiskWarning to be active")
	}
	if reasons[string(api.DeviceDisconnected)] == nil || reasons[string(api.DeviceDisconnected)].EndsAt != nil {
		t.Errorf("expected DeviceDisconnected to be active")
	}
	if len(checkpointCtx.updatedAlerts) != 4 {
		t.Errorf("expected 4 alerts to be updated")
	}
}

func TestClearAlertGroup(t *testing.T) {
	now := time.Now()
	event := fakeEvent("org", "Device", "dev1", string(api.DeviceMemoryNormal))
	key := AlertKeyFromEvent(event)
	alerts := map[AlertKey]map[string]*AlertInfo{
		key: {
			string(api.DeviceMemoryWarning):  {StartsAt: now},
			string(api.DeviceMemoryCritical): {StartsAt: now},
		},
	}
	checkpointCtx := &CheckpointContext{
		alerts:        alerts,
		updatedAlerts: []*AlertInfo{},
	}
	checkpointCtx.clearAlertGroup(event, memoryGroup)

	if alerts[key][string(api.DeviceMemoryWarning)].EndsAt == nil {
		t.Errorf("expected DeviceMemoryWarning to be resolved")
	}
	if alerts[key][string(api.DeviceMemoryCritical)].EndsAt == nil {
		t.Errorf("expected DeviceMemoryCritical to be resolved")
	}
	if len(checkpointCtx.updatedAlerts) != 2 {
		t.Errorf("expected 2 alerts to be updated")
	}
}

func TestProcessEvent_AppStatus(t *testing.T) {
	event := fakeEvent("org", "Device", "dev1", string(api.DeviceApplicationError))
	key := AlertKeyFromEvent(event)
	checkpointCtx := &CheckpointContext{
		alerts:        make(map[AlertKey]map[string]*AlertInfo),
		updatedAlerts: []*AlertInfo{},
	}

	checkpointCtx.processEvent(event)

	reasons := checkpointCtx.alerts[key]
	if reasons[string(api.DeviceApplicationError)].EndsAt != nil || reasons[string(api.DeviceApplicationError)].StartsAt != *event.Metadata.CreationTimestamp {
		t.Errorf("expected DeviceApplicationError to be set")
	}
	if len(checkpointCtx.updatedAlerts) != 1 {
		t.Errorf("expected 1 alert to be updated")
	}
}

func TestProcessEvent_AppHealthy(t *testing.T) {
	event := fakeEvent("org", "Device", "dev1", string(api.DeviceApplicationHealthy))
	key := AlertKeyFromEvent(event)
	alerts := map[AlertKey]map[string]*AlertInfo{
		key: {
			string(api.DeviceApplicationError):    {StartsAt: time.Now()},
			string(api.DeviceApplicationDegraded): {StartsAt: time.Now()},
		},
	}

	checkpointCtx := &CheckpointContext{
		alerts:        alerts,
		updatedAlerts: []*AlertInfo{},
	}
	checkpointCtx.processEvent(event)

	if alerts[key][string(api.DeviceApplicationError)].EndsAt == nil {
		t.Errorf("expected DeviceApplicationError to be resolved")
	}
	if alerts[key][string(api.DeviceApplicationDegraded)].EndsAt == nil {
		t.Errorf("expected DeviceApplicationDegraded to be resolved")
	}
	if len(checkpointCtx.updatedAlerts) != 2 {
		t.Errorf("expected 2 alerts to be updated")
	}
}

func TestProcessEvent_Connected(t *testing.T) {
	event := fakeEvent("org", "Device", "dev1", string(api.DeviceConnected))
	key := AlertKeyFromEvent(event)
	alerts := map[AlertKey]map[string]*AlertInfo{
		key: {string(api.DeviceDisconnected): {StartsAt: time.Now()}},
	}

	checkpointCtx := &CheckpointContext{
		alerts:        alerts,
		updatedAlerts: []*AlertInfo{},
	}
	checkpointCtx.processEvent(event)

	if alerts[key][string(api.DeviceDisconnected)].EndsAt == nil {
		t.Errorf("expected DeviceDisconnected to be resolved")
	}
	if len(checkpointCtx.updatedAlerts) != 1 {
		t.Errorf("expected 1 alert to be updated")
	}
}

func TestProcessEvent_ResourceDeleted(t *testing.T) {
	event := fakeEvent("org", "Device", "dev1", string(api.ResourceDeleted))
	key := AlertKeyFromEvent(event)

	alerts := map[AlertKey]map[string]*AlertInfo{
		key: {
			string(api.DeviceMemoryWarning): {StartsAt: time.Now()},
			string(api.DeviceDiskCritical):  {StartsAt: time.Now()},
		},
	}

	checkpointCtx := &CheckpointContext{
		alerts:        alerts,
		updatedAlerts: []*AlertInfo{},
	}
	checkpointCtx.processEvent(event)

	if alerts[key][string(api.DeviceMemoryWarning)].EndsAt == nil {
		t.Errorf("expected DeviceMemoryWarning to be resolved")
	}
	if alerts[key][string(api.DeviceDiskCritical)].EndsAt == nil {
		t.Errorf("expected DeviceDiskCritical to be resolved")
	}
	if len(checkpointCtx.updatedAlerts) != 2 {
		t.Errorf("expected 2 alerts to be updated")
	}
}

func fakeEvent(org, kind, name, reason string) api.Event {
	return api.Event{
		Metadata: api.ObjectMeta{
			CreationTimestamp: lo.ToPtr(time.Now()),
		},
		Reason: api.EventReason(reason),
		InvolvedObject: api.ObjectReference{
			Kind: kind,
			Name: name,
		},
	}
}
