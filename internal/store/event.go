package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event interface {
	InitialMigration() error

	Create(ctx context.Context, orgId uuid.UUID, event *api.Event) error
	List(ctx context.Context, orgId uuid.UUID, listParams api.ListEventsParams) (*api.EventList, error)
}

type EventStore struct {
	db *gorm.DB
}

// Make sure we conform to Event interface
var _ Event = (*EventStore)(nil)

func NewEvent(db *gorm.DB) Event {
	return &EventStore{db: db}
}

func (s *EventStore) InitialMigration() error {
	if err := s.db.AutoMigrate(&model.Event{}); err != nil {
		return err
	}
	return nil
}

func (s *EventStore) Create(ctx context.Context, orgId uuid.UUID, resource *api.Event) error {
	m := model.NewEventFromApiResource(resource)
	m.OrgID = orgId
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Create(&m).Error
}

// List fetches events with filters and pagination
func (s *EventStore) List(ctx context.Context, orgId uuid.UUID, listParams api.ListEventsParams) (*api.EventList, error) {
	var events []model.Event

	// Start query with base conditions
	query := s.db.Model(&model.Event{}).Where("org_id = ?", orgId)

	// Apply filters
	query = applyFilters(query, listParams)

	// Default limit if not specified
	limit := 100
	if listParams.Limit != nil {
		limit = int(*listParams.Limit)
	}

	// Apply pagination: Decode Continue Token
	if listParams.Continue != nil {
		lastTimestamp, lastID, err := decodeContinueToken(*listParams.Continue)
		if err != nil {
			return nil, flterrors.ErrInvalidContinueToken
		}
		query = query.Where("timestamp < ? OR (timestamp = ? AND id < ?)", lastTimestamp, lastTimestamp, lastID)
	}

	// Fetch limit+1 to check if there are more events
	query = query.Order("timestamp DESC, id DESC").Limit(limit + 1)

	// Execute query
	err := query.Find(&events).Error
	if err != nil {
		return nil, ErrorFromGormError(err)
	}

	// Determine "Continue" token & Remaining Count
	var nextContinue *string
	var numRemaining *int64

	if len(events) > limit {
		// More events exist, so set the continue token and trim results
		lastEvent := events[limit]
		nextToken := encodeContinueToken(lastEvent.Timestamp, lastEvent.ID)
		nextContinue = &nextToken
		events = events[:limit] // Trim to requested limit

		// Count remaining items
		var remaining int64
		countQuery := s.db.Model(&model.Event{}).Where("org_id = ?", orgId)
		countQuery = applyFilters(countQuery, listParams)
		countQuery = countQuery.Where("timestamp < ? OR (timestamp = ? AND id < ?)", lastEvent.Timestamp, lastEvent.Timestamp, lastEvent.ID)

		if err := countQuery.Count(&remaining).Error; err != nil {
			return nil, ErrorFromGormError(err)
		}
		numRemaining = &remaining
	}

	apiList, err := model.EventsToApiResource(events, nextContinue, numRemaining)
	return &apiList, err
}

// 🔹 Apply filters for both list and count queries
func applyFilters(query *gorm.DB, listParams api.ListEventsParams) *gorm.DB {
	if listParams.Kind != nil {
		query = query.Where("resource_type = ?", *listParams.Kind)
	}
	if listParams.Name != nil {
		query = query.Where("resource_name = ?", *listParams.Name)
	}
	if listParams.CorrelationId != nil {
		query = query.Where("correlation_id = ?", *listParams.CorrelationId)
	}
	if listParams.Severity != nil {
		query = query.Where("severity = ?", *listParams.Severity)
	}
	if listParams.StartTime != nil {
		query = query.Where("timestamp >= ?", *listParams.StartTime)
	}
	if listParams.EndTime != nil {
		query = query.Where("timestamp <= ?", *listParams.EndTime)
	}
	return query
}

// 🔹 Encode continue token (timestamp + ID) into a safe string
func encodeContinueToken(timestamp time.Time, id uuid.UUID) string {
	token := fmt.Sprintf("%d|%s", timestamp.UnixNano(), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

// 🔹 Decode continue token (timestamp + ID) from a string
func decodeContinueToken(token string) (time.Time, uuid.UUID, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid continue token")
	}

	parts := strings.Split(string(data), "|")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("malformed continue token")
	}

	// Parse timestamp
	timestampInt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid timestamp in continue token")
	}

	// Parse UUID
	eventID, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid event ID in continue token")
	}

	return time.Unix(0, timestampInt), eventID, nil
}
