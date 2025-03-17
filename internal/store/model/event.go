package model

import (
	"encoding/json"
	"time"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event struct {
	ID            string                       `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	OrgID         uuid.UUID                    `gorm:"type:uuid;primaryKey;index"`
	Timestamp     time.Time                    `gorm:"autoCreateTime;index"`
	EventType     string                       `gorm:"type:varchar(100);index"`
	Source        string                       `gorm:"type:varchar(100)"`
	ActorUser     *string                      `gorm:"type:varchar(100);nullable"`
	ActorService  *string                      `gorm:"type:varchar(100);nullable"`
	Status        string                       `gorm:"type:varchar(20);index"`
	Severity      string                       `gorm:"type:varchar(20);index"`
	Message       string                       `gorm:"type:text"`
	Details       *JSONField[api.EventDetails] `gorm:"type:jsonb"`
	CorrelationID *string                      `gorm:"type:varchar(100);nullable"`
	ResourceName  string                       `gorm:"type:uuid;index"`
	ResourceType  string                       `gorm:"type:varchar(50);index"`
	CreatedAt     time.Time                    `gorm:"autoCreateTime"`
	DeletedAt     gorm.DeletedAt               `gorm:"index"`
}

func (e Event) String() string {
	val, _ := json.Marshal(e)
	return string(val)
}

func NewEventFromApiResource(resource *api.Event) *Event {
	if resource == nil {
		return &Event{}
	}
	details := api.EventDetails{}
	if resource.Details != nil {
		details = *resource.Details
	}
	return &Event{
		EventType:     string(resource.Type),
		Source:        string(resource.Source),
		ActorUser:     resource.ActorUser,
		ActorService:  resource.ActorService,
		Status:        string(resource.Status),
		Severity:      string(resource.Severity),
		Message:       resource.Message,
		Details:       MakeJSONField(details),
		CorrelationID: resource.CorrelationId,
		ResourceName:  resource.ResourceName,
		ResourceType:  string(resource.ResourceType),
	}
}
