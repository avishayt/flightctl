package store

import (
	"context"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event interface {
	InitialMigration() error

	Create(ctx context.Context, orgId uuid.UUID, event *api.Event) error
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
	return s.db.WithContext(ctx).Create(&m).Error
}
