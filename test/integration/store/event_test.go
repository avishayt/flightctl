package store_test

import (
	"context"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/store"
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
		now       time.Time
		events    []api.Event
	)

	BeforeEach(func() {
		ctx = context.Background()
		orgId, _ = uuid.NewUUID()
		log = flightlog.InitLogs()
		storeInst, cfg, dbName, _ = store.PrepareDBForUnitTests(log)

		now = time.Now().UTC()
		events = []api.Event{
			{
				Timestamp:     now.Add(-10 * time.Minute),
				Type:          api.EventTypeResourceCreated,
				Severity:      api.EventSeverityInfo,
				CorrelationId: lo.ToPtr("123"),
				Message:       "Resource created",
			},
			{
				Timestamp:     now.Add(-5 * time.Minute),
				Type:          api.EventTypeResourceUpdated,
				Severity:      api.EventSeverityWarning,
				CorrelationId: lo.ToPtr("123"),
				Message:       "Resource updated",
			},
			{
				Timestamp:     now.Add(-1 * time.Minute),
				Type:          api.EventTypeResourceDeleted,
				Severity:      api.EventSeverityCritical,
				CorrelationId: lo.ToPtr("456"),
				Message:       "Resource deleted",
			},
		}

		// Insert test events
		for _, event := range events {
			err := storeInst.Event().Create(ctx, orgId, &event)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	AfterEach(func() {
		store.DeleteTestDB(log, cfg, storeInst, dbName)
	})

	Context("Event Store", func() {
		It("List all events", func() {
			listParams := api.ListEventsParams{Limit: lo.ToPtr(int32(100))}
			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(eventList.Items).To(HaveLen(len(events)))

			// Verify order (should be descending by timestamp)
			Expect(eventList.Items[0].Type).To(Equal(api.EventTypeResourceDeleted))
			Expect(eventList.Items[1].Type).To(Equal(api.EventTypeResourceUpdated))
			Expect(eventList.Items[2].Type).To(Equal(api.EventTypeResourceCreated))
		})

		It("Filters events by severity", func() {
			// List only critical events
			listParams := api.ListEventsParams{
				Severity: lo.ToPtr(api.ListEventsParamsSeverityCritical),
				Limit:    lo.ToPtr(int32(10)),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(1))
			Expect(eventList.Items[0].Type).To(Equal(api.EventTypeResourceDeleted))
		})

		It("Filters events by time range", func() {
			startTime := now.Add(-7 * time.Minute)
			endTime := now.Add(-2 * time.Minute)

			listParams := api.ListEventsParams{
				StartTime: &startTime,
				EndTime:   &endTime,
				Limit:     lo.ToPtr(int32(10)),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(1))
			Expect(eventList.Items[0].Type).To(Equal(api.EventTypeResourceUpdated))
		})

		It("Filters events by correlation ID", func() {
			listParams := api.ListEventsParams{
				CorrelationId: lo.ToPtr("123"),
				Limit:         lo.ToPtr(int32(10)),
			}

			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList.Items)).To(Equal(2))
			Expect(eventList.Items[0].Type).To(Equal(api.EventTypeResourceUpdated))
			Expect(eventList.Items[1].Type).To(Equal(api.EventTypeResourceCreated))
		})

		It("Paginates events correctly", func() {
			// List first event with limit 1
			listParams := api.ListEventsParams{Limit: lo.ToPtr(int32(1))}
			eventList, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())

			Expect(len(eventList.Items)).To(Equal(1))
			Expect(eventList.Metadata.Continue).ToNot(BeNil())

			// Fetch next page using continue token
			listParams.Continue = eventList.Metadata.Continue
			eventList2, err := storeInst.Event().List(ctx, orgId, listParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(eventList2.Items)).To(Equal(1))

			// Ensure events are different across pages
			Expect(eventList.Items[0].Type).ToNot(Equal(eventList2.Items[0].Type))
		})
	})
})
