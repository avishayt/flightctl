package alert_exporter_test

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/alert_exporter"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/tasks_client"
	flightlog "github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/pkg/queues"
	testutil "github.com/flightctl/flightctl/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"go.uber.org/mock/gomock"
	"gorm.io/gorm"
)

var (
	suiteCtx context.Context
)

func TestExporterIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alert Exporter Integration Suite")
}

var _ = BeforeSuite(func() {
	suiteCtx = testutil.InitSuiteTracerForGinkgo("Tasks Suite")
})

var _ = Describe("Alert Exporter", func() {
	var (
		log             *logrus.Logger
		ctx             context.Context
		storeInst       store.Store
		serviceHandler  service.Service
		cfg             *config.Config
		db              *gorm.DB
		dbName          string
		callbackManager tasks_client.CallbackManager
		mockPublisher   *queues.MockPublisher
		ctrl            *gomock.Controller
		alertPoller     *alert_exporter.EventPoller
	)

	BeforeEach(func() {
		ctx = testutil.StartSpecTracerForGinkgo(suiteCtx)
		log = flightlog.InitLogs()
		storeInst, cfg, dbName, db = store.PrepareDBForUnitTests(ctx, log)
		ctrl = gomock.NewController(GinkgoT())
		mockPublisher = queues.NewMockPublisher(ctrl)
		callbackManager = tasks_client.NewCallbackManager(mockPublisher, log)
		mockPublisher.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()
		kvStore, err := kvstore.NewKVStore(ctx, log, "localhost", 6379, "adminpass")
		Expect(err).ToNot(HaveOccurred())
		serviceHandler = service.NewServiceHandler(storeInst, callbackManager, kvStore, nil, log, "", "")
		alertPoller = alert_exporter.NewEventPoller(log, serviceHandler, 100*time.Millisecond)
		alert_exporter.ResetActiveAlerts()
	})

	AfterEach(func() {
		store.DeleteTestDB(ctx, log, cfg, storeInst, dbName)
		ctrl.Finish()
	})

	It("replays events if the checkpoint is deleted", func() {
		replayEventsFromFreshState(ctx, db, serviceHandler, alertPoller, func() bool {
			err := db.WithContext(ctx).Exec(`
			DELETE FROM checkpoints
			WHERE consumer = ? AND key = ?`, alert_exporter.AlertCheckpointConsumer, alert_exporter.AlertCheckpointKey).Error
			Expect(err).ToNot(HaveOccurred())
			return true
		})
	})

	It("replays events if the checkpoint is garbage", func() {
		replayEventsFromFreshState(ctx, db, serviceHandler, alertPoller, func() bool {
			err := db.WithContext(ctx).Exec(`
			UPDATE checkpoints SET value = 'corrupted json here'
			WHERE consumer = ? AND key = ?`, alert_exporter.AlertCheckpointConsumer, alert_exporter.AlertCheckpointKey).Error
			Expect(err).ToNot(HaveOccurred())
			return true
		})
	})

	It("starts fresh if the checkpoint and all events are deleted", func() {
		replayEventsFromFreshState(ctx, db, serviceHandler, alertPoller, func() bool {
			err := db.WithContext(ctx).Exec(`
			DELETE FROM checkpoints WHERE consumer = ? AND key = ?`, alert_exporter.AlertCheckpointConsumer, alert_exporter.AlertCheckpointKey).Error
			Expect(err).ToNot(HaveOccurred())

			err = db.WithContext(ctx).Exec(`DELETE FROM events`).Error
			Expect(err).ToNot(HaveOccurred())
			return false
		})
	})

	It("publishes a metric when a relevant event occurs", func() {
		alertPoller.LoadCheckpoint(ctx)
		params := alertPoller.GetListEventsParams()
		params.Limit = lo.ToPtr(int32(2))

		createEvent(ctx, serviceHandler, api.DeviceCPUWarning, api.DeviceKind, "dev1")
		createEvent(ctx, serviceHandler, api.ResourceCreated, api.FleetKind, "flt1")
		createEvent(ctx, serviceHandler, api.DeviceConnected, api.DeviceKind, "dev2")

		alertPoller.ProcessLatestEvents(ctx, params)
		metrics := getMetrics()
		Expect(metrics).To(HaveLen(1))
		Expect(metrics[0]).To(Equal(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev1", reason="DeviceCPUWarning"} 1`))
	})

	It("clears an alert when the resource is deleted", func() {
		alertPoller.LoadCheckpoint(ctx)
		params := alertPoller.GetListEventsParams()
		params.Limit = lo.ToPtr(int32(2))

		createEvent(ctx, serviceHandler, api.DeviceCPUWarning, api.DeviceKind, "dev1")
		alertPoller.ProcessLatestEvents(ctx, params)
		metrics := getMetrics()
		Expect(metrics).To(HaveLen(1))
		Expect(metrics[0]).To(Equal(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev1", reason="DeviceCPUWarning"} 1`))

		createEvent(ctx, serviceHandler, api.ResourceDeleted, api.DeviceKind, "dev1")
		params = alertPoller.GetListEventsParams()
		params.Limit = lo.ToPtr(int32(2))
		alertPoller.ProcessLatestEvents(ctx, params)

		metrics = getMetrics()
		Expect(metrics).To(HaveLen(0))
	})

	It("clears alerts when they are resolved", func() {
		alertPoller.LoadCheckpoint(ctx)
		params := alertPoller.GetListEventsParams()
		params.Limit = lo.ToPtr(int32(2))
		createEvent(ctx, serviceHandler, api.DeviceCPUCritical, api.DeviceKind, "dev1")
		createEvent(ctx, serviceHandler, api.DeviceMemoryCritical, api.DeviceKind, "dev2")
		createEvent(ctx, serviceHandler, api.DeviceDiskCritical, api.DeviceKind, "dev3")
		createEvent(ctx, serviceHandler, api.DeviceApplicationError, api.DeviceKind, "dev4")
		createEvent(ctx, serviceHandler, api.DeviceDisconnected, api.DeviceKind, "dev5")
		alertPoller.ProcessLatestEvents(ctx, params)

		metrics := getMetrics()
		Expect(metrics).To(HaveLen(5))
		Expect(metrics).To(ContainElement(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev1", reason="DeviceCPUCritical"} 1`))
		Expect(metrics).To(ContainElement(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev2", reason="DeviceMemoryCritical"} 1`))
		Expect(metrics).To(ContainElement(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev3", reason="DeviceDiskCritical"} 1`))
		Expect(metrics).To(ContainElement(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev4", reason="DeviceApplicationError"} 1`))
		Expect(metrics).To(ContainElement(`fc_alert_active{org_id="00000000-0000-0000-0000-000000000000", resource_kind="Device", resource_name="dev5", reason="DeviceDisconnected"} 1`))

		createEvent(ctx, serviceHandler, api.DeviceCPUNormal, api.DeviceKind, "dev1")
		createEvent(ctx, serviceHandler, api.DeviceMemoryNormal, api.DeviceKind, "dev2")
		createEvent(ctx, serviceHandler, api.DeviceDiskNormal, api.DeviceKind, "dev3")
		createEvent(ctx, serviceHandler, api.DeviceApplicationHealthy, api.DeviceKind, "dev4")
		createEvent(ctx, serviceHandler, api.DeviceConnected, api.DeviceKind, "dev5")
		params = alertPoller.GetListEventsParams()
		params.Limit = lo.ToPtr(int32(2))
		alertPoller.ProcessLatestEvents(ctx, params)

		metrics = getMetrics()
		Expect(metrics).To(HaveLen(0))
	})
})

func createEvent(ctx context.Context, handler service.Service, reason api.EventReason, kind, name string) {
	ev := &api.Event{
		Reason:         reason,
		InvolvedObject: api.ObjectReference{Kind: kind, Name: name},
		Metadata:       api.ObjectMeta{Name: lo.ToPtr(fmt.Sprintf("test-event-%d", rand.Int63()))}, //nolint:gosec
	}
	time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	handler.CreateEvent(ctx, ev)
}

func getMetrics() []string {
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	alert_exporter.MetricsHandler(w, req)
	out := strings.TrimSpace(w.Body.String())
	if out == "" {
		return []string{}
	}
	return strings.Split(out, "\n")
}

func replayEventsFromFreshState(ctx context.Context, db *gorm.DB, serviceHandler service.Service, poller *alert_exporter.EventPoller, checkpointSetup func() bool) {
	// Add an alert for dev1
	poller.LoadCheckpoint(ctx)
	params := poller.GetListEventsParams()
	createEvent(ctx, serviceHandler, api.DeviceCPUWarning, api.DeviceKind, "dev1")
	poller.ProcessLatestEvents(ctx, params)

	// Verify alert for dev1 exists
	checkpoint, status := serviceHandler.GetCheckpoint(ctx, alert_exporter.AlertCheckpointConsumer, alert_exporter.AlertCheckpointKey)
	Expect(status.Code).To(Equal(int32(http.StatusOK)))
	Expect(checkpoint).ToNot(BeNil())
	Expect(string(checkpoint)).To(ContainSubstring(`"DeviceCPUWarning"`))

	// Apply scenario-specific setup (e.g., delete or corrupt checkpoint)
	firstAlertShouldExist := checkpointSetup()

	// Replay events for dev2
	poller.LoadCheckpoint(ctx)
	params = poller.GetListEventsParams()
	createEvent(ctx, serviceHandler, api.DeviceMemoryWarning, api.DeviceKind, "dev2")
	poller.ProcessLatestEvents(ctx, params)

	// Validate both dev1 and dev2 alerts are present
	checkpoint, status = serviceHandler.GetCheckpoint(ctx, alert_exporter.AlertCheckpointConsumer, alert_exporter.AlertCheckpointKey)
	Expect(status.Code).To(Equal(int32(http.StatusOK)))
	Expect(checkpoint).ToNot(BeNil())
	Expect(string(checkpoint)).To(ContainSubstring(`"DeviceMemoryWarning"`))
	if firstAlertShouldExist {
		Expect(string(checkpoint)).To(ContainSubstring(`"DeviceCPUWarning"`))
	} else {
		Expect(string(checkpoint)).ToNot(ContainSubstring(`"DeviceCPUWarning"`))
	}
}
