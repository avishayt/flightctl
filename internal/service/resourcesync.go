package service

import (
	"context"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/server"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/labels"
)

type ResourceSyncStoreInterface interface {
	CreateResourceSync(ctx context.Context, orgId uuid.UUID, repository *api.ResourceSync) (*api.ResourceSync, error)
	ListResourceSync(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.ResourceSyncList, error)
	ListAllResourceSyncInternal() ([]model.ResourceSync, error)
	DeleteResourceSyncs(ctx context.Context, orgId uuid.UUID) error
	GetResourceSync(ctx context.Context, orgId uuid.UUID, name string) (*api.ResourceSync, error)
	CreateOrUpdateResourceSync(ctx context.Context, orgId uuid.UUID, repository *api.ResourceSync) (*api.ResourceSync, bool, error)
	DeleteResourceSync(ctx context.Context, orgId uuid.UUID, name string) error
	UpdateResourceSyncStatusInternal(resourceSync *model.ResourceSync) error
}

// (POST /api/v1/resourcesyncs)
func (h *ServiceHandler) CreateResourceSync(ctx context.Context, request server.CreateResourceSyncRequestObject) (server.CreateResourceSyncResponseObject, error) {
	orgId := NullOrgId

	result, err := h.resourceSyncStore.CreateResourceSync(ctx, orgId, request.Body)
	switch err {
	case nil:
		return server.CreateResourceSync201JSONResponse(*result), nil
	default:
		return nil, err
	}
}

// (GET /api/v1/resourcesyncs)
func (h *ServiceHandler) ListResourceSync(ctx context.Context, request server.ListResourceSyncRequestObject) (server.ListResourceSyncResponseObject, error) {
	orgId := NullOrgId
	labelSelector := ""
	if request.Params.LabelSelector != nil {
		labelSelector = *request.Params.LabelSelector
	}

	labelMap, err := labels.ConvertSelectorToLabelsMap(labelSelector)
	if err != nil {
		return nil, err
	}

	cont, err := ParseContinueString(request.Params.Continue)
	if err != nil {
		return server.ListResourceSync400Response{}, fmt.Errorf("failed to parse continue parameter: %s", err)
	}

	listParams := ListParams{
		Labels:   labelMap,
		Limit:    int(swag.Int32Value(request.Params.Limit)),
		Continue: cont,
	}
	if listParams.Limit == 0 {
		listParams.Limit = MaxRecordsPerListRequest
	}
	if listParams.Limit > MaxRecordsPerListRequest {
		return server.ListResourceSync400Response{}, fmt.Errorf("limit cannot exceed %d", MaxRecordsPerListRequest)
	}

	result, err := h.resourceSyncStore.ListResourceSync(ctx, orgId, listParams)
	switch err {
	case nil:
		return server.ListResourceSync200JSONResponse(*result), nil
	default:
		return nil, err
	}
}

// (DELETE /api/v1/resourcesyncs)
func (h *ServiceHandler) DeleteResourceSyncs(ctx context.Context, request server.DeleteResourceSyncsRequestObject) (server.DeleteResourceSyncsResponseObject, error) {
	orgId := NullOrgId

	err := h.resourceSyncStore.DeleteResourceSyncs(ctx, orgId)
	switch err {
	case nil:
		return server.DeleteResourceSyncs200JSONResponse{}, nil
	default:
		return nil, err
	}
}

// (GET /api/v1/resourcesyncs/{name})
func (h *ServiceHandler) ReadResourceSync(ctx context.Context, request server.ReadResourceSyncRequestObject) (server.ReadResourceSyncResponseObject, error) {
	orgId := NullOrgId

	result, err := h.resourceSyncStore.GetResourceSync(ctx, orgId, request.Name)
	switch err {
	case nil:
		return server.ReadResourceSync200JSONResponse(*result), nil
	case gorm.ErrRecordNotFound:
		return server.ReadResourceSync404Response{}, nil
	default:
		return nil, err
	}
}

// (PUT /api/v1/resourcesyncs/{name})
func (h *ServiceHandler) ReplaceResourceSync(ctx context.Context, request server.ReplaceResourceSyncRequestObject) (server.ReplaceResourceSyncResponseObject, error) {
	orgId := NullOrgId
	if request.Body.Metadata.Name == nil || request.Name != *request.Body.Metadata.Name {
		return server.ReplaceResourceSync400Response{}, nil
	}

	result, created, err := h.resourceSyncStore.CreateOrUpdateResourceSync(ctx, orgId, request.Body)
	switch err {
	case nil:
		if created {
			return server.ReplaceResourceSync201JSONResponse(*result), nil
		} else {
			return server.ReplaceResourceSync200JSONResponse(*result), nil
		}
	case gorm.ErrRecordNotFound:
		return server.ReplaceResourceSync404Response{}, nil
	default:
		return nil, err
	}
}

// (DELETE /api/v1/resourcesyncs/{name})
func (h *ServiceHandler) DeleteResourceSync(ctx context.Context, request server.DeleteResourceSyncRequestObject) (server.DeleteResourceSyncResponseObject, error) {
	orgId := NullOrgId

	err := h.resourceSyncStore.DeleteResourceSync(ctx, orgId, request.Name)
	switch err {
	case nil:
		return server.DeleteResourceSync200JSONResponse{}, nil
	case gorm.ErrRecordNotFound:
		return server.DeleteResourceSync404Response{}, nil
	default:
		return nil, err
	}
}
