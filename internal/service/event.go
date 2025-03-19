package service

import (
	"context"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/samber/lo"
)

func (h *ServiceHandler) CreateEvent(ctx context.Context, event api.Event) {
	orgId := store.NullOrgId

	err := h.store.Event().Create(ctx, orgId, &event)
	if err != nil {
		h.log.Errorf("failed emitting resource updated event for %s %s/%s: %v", event.ResourceKind, orgId, event.ResourceName, err)
	}
}

func (h *ServiceHandler) ListEvents(ctx context.Context, params api.ListEventsParams) (*api.EventList, api.Status) {
	orgId := store.NullOrgId

	if params.Limit == nil || *params.Limit == 0 {
		params.Limit = lo.ToPtr(int32(MaxRecordsPerListRequest))
	} else if *params.Limit > MaxRecordsPerListRequest {
		return nil, api.StatusBadRequest(fmt.Sprintf("limit cannot exceed %d", MaxRecordsPerListRequest))
	} else if *params.Limit < 0 {
		return nil, api.StatusBadRequest("limit cannot be negative")
	}

	result, err := h.store.Event().List(ctx, orgId, params)
	return result, StoreErrorToApiStatus(err, false, api.EventKind, nil)
}
