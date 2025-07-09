package tasks

import (
	"context"
	"fmt"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/store/selector"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const (
	// DeviceDisconnectedPollingInterval is the interval at which the device liveness task runs.
	DeviceDisconnectedPollingInterval = 2 * time.Minute
)

type DeviceDisconnected struct {
	log            logrus.FieldLogger
	serviceHandler service.Service
	store          store.Store
}

func NewDeviceDisconnected(log logrus.FieldLogger, serviceHandler service.Service, store store.Store) *DeviceDisconnected {
	return &DeviceDisconnected{
		log:            log,
		serviceHandler: serviceHandler,
		store:          store,
	}
}

// Poll checks the status of devices and updates the status to unknown if the device has not reported in the last DeviceDisconnectedTimeout.
func (t *DeviceDisconnected) Poll(ctx context.Context) {
	t.log.Info("Running DeviceDisconnected Polling")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Calculate the cutoff time for disconnected devices
	cutoffTime := time.Now().Add(-api.DeviceDisconnectedTimeout)

	// Create a field selector to only get devices that haven't been seen for more than DeviceDisconnectedTimeout
	// and don't already have "Unknown" status to avoid reprocessing the same devices
	fieldSelectorStr := fmt.Sprintf("status.lastSeen<%s,status.summary.status!=Unknown", cutoffTime.Format(time.RFC3339))
	fieldSelector, err := selector.NewFieldSelector(fieldSelectorStr)
	if err != nil {
		t.log.Errorf("Failed to create field selector: %v", err)
		return
	}

	// List devices that match the disconnection criteria
	devices, err := t.store.Device().List(ctx, store.NullOrgId, store.ListParams{
		FieldSelector: fieldSelector,
		Limit:         ItemsPerPage,
	})
	if err != nil {
		t.log.Errorf("Failed to list devices: %v", err)
		return
	}

	if len(devices.Items) == 0 {
		return
	}

	t.log.Infof("Updating %d devices to unknown status", len(devices.Items))

	// Extract device names for batch update
	deviceNames := make([]string, len(devices.Items))
	statusUpdates := make(map[string]store.StatusFieldUpdate)

	for i, device := range devices.Items {
		deviceNames[i] = *device.Metadata.Name

		// Update the device status using service side logic
		t.serviceHandler.UpdateServiceSideDeviceStatus(ctx, device)

		// The service side update should have set the status fields correctly
		// Extract the updated values and use them in the batch update
		if statusUpdates["summary"] == (store.StatusFieldUpdate{}) {
			statusUpdates["summary"] = store.StatusFieldUpdate{
				Status: string(device.Status.Summary.Status),
				Info:   lo.FromPtr(device.Status.Summary.Info),
			}
		}
		if statusUpdates["updated"] == (store.StatusFieldUpdate{}) {
			statusUpdates["updated"] = store.StatusFieldUpdate{
				Status: string(device.Status.Updated.Status),
				Info:   lo.FromPtr(device.Status.Updated.Info),
			}
		}
		if statusUpdates["applicationsSummary"] == (store.StatusFieldUpdate{}) {
			statusUpdates["applicationsSummary"] = store.StatusFieldUpdate{
				Status: string(device.Status.ApplicationsSummary.Status),
				Info:   lo.FromPtr(device.Status.ApplicationsSummary.Info),
			}
		}
	}

	// Update all devices in batch
	if len(deviceNames) > 0 {
		if err := t.store.Device().UpdateStatusFieldsBatch(ctx, store.NullOrgId, deviceNames, statusUpdates); err != nil {
			t.log.Errorf("Failed to update device status batch: %v", err)
		}
	}
}
