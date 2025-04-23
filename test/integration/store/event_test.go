package store_test

import (
	"context"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/store/selector"
	flightlog "github.com/flightctl/flightctl/pkg/log"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

var _ = Describe("EventStore Integration Tests", func() {
	var (
		log       *logrus.Logger
		ctx       context.Context
		orgId     uuid.UUID
		storeInst store.Store
		cfg       *config.Config
		dbName    string
		events    []api.Event
	)

	BeforeEach(func() {
		ctx = context.Background()
		orgId, _ = uuid.NewUUID()
		log = flightlog.InitLogs()
		storeInst, cfg, dbName, _ = store.PrepareDBForUnitTests(log)

		events = []api.Event{
			{
				Metadata: api.ObjectMeta{
					Name: lo.ToPtr("event-1"),
				},
				Type:          api.EventTypeNormal,
				Reason:        api.EventReasonResourceCreationSucceeded,
				CorrelationId: "123",
				Message:       "Resource created",
				InvolvedObject: api.ObjectReference{
					Kind: api.DeviceKind,
					Name: "my-device",
				},
				Actor: "user:admin",
			},
			{
				Metadata: api.ObjectMeta{
					Name: lo.ToPtr("event-2"),
				},
				Type:          api.EventTypeNormal,
				Reason:        api.EventReasonResourceUpdateSucceeded,
				CorrelationId: "456",
				Message:       "Resource updated",
				InvolvedObject: api.ObjectReference{
					Kind: api.FleetKind,
					Name: "my-fleet",
				},
				Actor: "user:admin",
			},
			{
				Metadata: api.ObjectMeta{
					Name: lo.ToPtr("event-3"),
				},
				Type:          api.EventTypeNormal,
				Reason:        api.EventReasonResourceDeletionSucceeded,
				CorrelationId: "123",
				Message:       "Resource deleted",
				InvolvedObject: api.ObjectReference{
					Kind: api.DeviceKind,
					Name: "my-device",
				},
				Actor: "service:device-controller",
			},
		}

		// Insert test events
		for _, event := range events {
			err := storeInst.Event().Create(ctx, orgId, &event)
			time.Sleep(10 * time.Microsecond) // Ensure different timestamps
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		store.DeleteTestDB(log, cfg, storeInst, dbName)
	})

	Context("Event Store", func() {
		It("List all events", func() {
			listParams := store.ListParams{Limit: 100}
			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(eventList.Items).To(HaveLen(len(events)))

			// Verify order (should be descending by timestamp - newest first)
			Expect(eventList.Items[0].Reason).To(Equal(api.EventReasonResourceDeletionSucceeded))
			Expect(eventList.Items[1].Reason).To(Equal(api.EventReasonResourceUpdateSucceeded))
			Expect(eventList.Items[2].Reason).To(Equal(api.EventReasonResourceCreationSucceeded))
		})

		It("Filters events by reason", func() {
			listParams := store.ListParams{
				Limit: 100,
				FieldSelector: selector.NewFieldSelectorFromMapOrDie(
					map[string]string{"reason": string(api.EventReasonResourceDeletionSucceeded)}, selector.WithPrivateSelectors()),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(1))
			Expect(eventList.Items[0].Reason).To(Equal(api.EventReasonResourceDeletionSucceeded))
		})

		It("Filters events by correlation ID", func() {
			listParams := store.ListParams{
				Limit: 100,
				FieldSelector: selector.NewFieldSelectorFromMapOrDie(
					map[string]string{"correlationId": "123"}, selector.WithPrivateSelectors()),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(2))
			Expect(eventList.Items[0].Reason).To(Equal(api.EventReasonResourceDeletionSucceeded))
			Expect(eventList.Items[1].Reason).To(Equal(api.EventReasonResourceCreationSucceeded))
		})

		It("Filters events by actor", func() {
			listParams := store.ListParams{
				Limit: 100,
				FieldSelector: selector.NewFieldSelectorFromMapOrDie(
					map[string]string{"actor": "user:admin"}, selector.WithPrivateSelectors()),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(2))
			Expect(eventList.Items[0].Reason).To(Equal(api.EventReasonResourceUpdateSucceeded))
			Expect(eventList.Items[1].Reason).To(Equal(api.EventReasonResourceCreationSucceeded))
		})

		It("Filters events by involved object", func() {
			listParams := store.ListParams{
				Limit: 100,
				FieldSelector: selector.NewFieldSelectorFromMapOrDie(
					map[string]string{"involvedObject.kind": string(api.DeviceKind), "involvedObject.name": "my-device"},
					selector.WithPrivateSelectors()),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(2))
			Expect(eventList.Items[0].Reason).To(Equal(api.EventReasonResourceDeletionSucceeded))
			Expect(eventList.Items[1].Reason).To(Equal(api.EventReasonResourceCreationSucceeded))
		})
	})
})
