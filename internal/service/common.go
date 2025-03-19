package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/flightctl/flightctl/api/v1alpha1"
	api "github.com/flightctl/flightctl/api/v1alpha1"
	commonauth "github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

type ctxKey string

const (
	MaxRecordsPerListRequest        = 1000
	InternalRequestCtxKey    ctxKey = "internal_request"
	EventSourceCtxKey        ctxKey = "event_source"
	DelayDeviceRenderCtxKey  ctxKey = "delayDeviceRender"
)

func IsInternalRequest(ctx context.Context) bool {
	if internal, ok := ctx.Value(InternalRequestCtxKey).(bool); ok && internal {
		return true
	}
	return false
}

func NilOutManagedObjectMetaProperties(om *v1alpha1.ObjectMeta) {
	if om == nil {
		return
	}
	om.Generation = nil
	om.Owner = nil
	om.Annotations = nil
	om.CreationTimestamp = nil
	om.DeletionTimestamp = nil
}

func validateAgainstSchema(ctx context.Context, obj []byte, objPath string) error {
	swagger, err := v1alpha1.GetSwagger()
	if err != nil {
		return err
	}
	// Skip server name validation
	swagger.Servers = nil

	url, err := url.Parse(objPath)
	if err != nil {
		return err
	}
	httpReq := &http.Request{
		Method: "PUT",
		URL:    url,
		Body:   io.NopCloser(bytes.NewReader(obj)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}

	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		return err
	}
	route, pathParams, err := router.FindRoute(httpReq)
	if err != nil {
		return err
	}

	requestValidationInput := &openapi3filter.RequestValidationInput{
		Request:    httpReq,
		PathParams: pathParams,
		Route:      route,
	}
	return openapi3filter.ValidateRequest(ctx, requestValidationInput)
}

func ApplyJSONPatch[T any](ctx context.Context, obj T, newObj T, patchRequest api.PatchRequest, objPath string) error {
	patch, err := json.Marshal(patchRequest)
	if err != nil {
		return err
	}
	jsonPatch, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		return err
	}

	objJSON, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	newJSON, err := jsonPatch.Apply(objJSON)
	if err != nil {
		return err
	}

	//validate the new object against OpenAPI schema
	err = validateAgainstSchema(ctx, newJSON, objPath)
	if err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(newJSON))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&newObj)
}

func StoreErrorToApiStatus(err error, created bool, kind string, name *string) api.Status {
	if err == nil {
		if created {
			return api.StatusCreated()
		}
		return api.StatusOK()
	}

	badRequestErrors := map[error]bool{
		flterrors.ErrResourceIsNil:                 true,
		flterrors.ErrResourceNameIsNil:             true,
		flterrors.ErrIllegalResourceVersionFormat:  true,
		flterrors.ErrFieldSelectorSyntax:           true,
		flterrors.ErrFieldSelectorParseFailed:      true,
		flterrors.ErrFieldSelectorUnknownSelector:  true,
		flterrors.ErrLabelSelectorSyntax:           true,
		flterrors.ErrLabelSelectorParseFailed:      true,
		flterrors.ErrAnnotationSelectorSyntax:      true,
		flterrors.ErrAnnotationSelectorParseFailed: true,
		flterrors.ErrInvalidContinueToken:          true,
	}

	conflictErrors := map[error]bool{
		flterrors.ErrUpdatingResourceWithOwnerNotAllowed: true,
		flterrors.ErrDuplicateName:                       true,
		flterrors.ErrNoRowsUpdated:                       true,
		flterrors.ErrResourceVersionConflict:             true,
		flterrors.ErrResourceOwnerIsNil:                  true,
		flterrors.ErrTemplateVersionIsNil:                true,
		flterrors.ErrInvalidTemplateVersion:              true,
		flterrors.ErrNoRenderedVersion:                   true,
		flterrors.ErrDecommission:                        true,
	}

	switch {
	case errors.Is(err, flterrors.ErrResourceNotFound):
		return api.StatusResourceNotFound(kind, util.DefaultIfNil(name, "none"))
	case badRequestErrors[err]:
		return api.StatusBadRequest(err.Error())
	case conflictErrors[err]:
		return api.StatusResourceVersionConflict(err.Error())
	default:
		return api.StatusInternalServerError(err.Error())
	}
}

func ApiStatusToErr(status api.Status) error {
	if status.Code >= 200 && status.Code < 300 {
		return nil
	}
	return errors.New(status.Message)
}

func ParseEventSource(s string) *api.EventSource {
	switch api.EventSource(s) {
	case api.DeviceAgent, api.ServiceApi, api.ServicePeriodic, api.ServiceTask:
		return lo.ToPtr(api.EventSource(s))
	default:
		return nil
	}
}

func GetCommonEvent(ctx context.Context, status api.Status, resourceName string, ResourceKind api.ResourceKind) *api.Event {
	event := api.Event{
		ResourceKind: ResourceKind,
		ResourceName: resourceName,
		Severity:     api.EventSeverityInfo,
	}

	if status.Code >= 200 && status.Code < 299 {
		event.Status = api.Success
	} else if status.Code >= 500 && status.Code < 599 {
		event.Status = api.Failure
	} else {
		// If it's not one of the above cases, it's 4XX, which we don't emit events for
		return nil
	}

	identity, err := commonauth.GetIdentity(ctx)
	if err == nil && identity != nil {
		event.ActorUser = &identity.Username
	}

	// Set correlationId to requestID
	requestID := ctx.Value(middleware.RequestIDKey)
	if requestID != nil {
		if reqIDStr, ok := requestID.(string); ok {
			event.CorrelationId = &reqIDStr
		}
	}

	source := ctx.Value(EventSourceCtxKey)
	if source != nil {
		if sourceStr, ok := source.(string); ok {
			e := ParseEventSource(sourceStr)
			if e != nil {
				event.Source = *e
				if *e == api.ServiceTask || *e == api.ServicePeriodic {
					event.ActorService = &sourceStr
				}
			}
		}
	}

	return &event
}

func GetDeviceDecommissionedEvent(ctx context.Context, eventStore store.Event, log logrus.FieldLogger, orgId uuid.UUID, status api.Status, resourceName string, ResourceKind api.ResourceKind) *api.Event {
	event := GetCommonEvent(ctx, status, resourceName, ResourceKind)
	if event == nil {
		return nil
	}
	event.Type = api.EventTypeDeviceDecommissioned
	if status.Code == http.StatusOK {
		event.Message = "Successfully decommissioned"
	} else {
		event.Message = status.Message
	}
	return event
}

func GetEnrollmentRequestApprovedEvent(ctx context.Context, eventStore store.Event, log logrus.FieldLogger, orgId uuid.UUID, status api.Status, resourceName string, ResourceKind api.ResourceKind) *api.Event {
	event := GetCommonEvent(ctx, status, resourceName, ResourceKind)
	if event == nil {
		return nil
	}
	event.Type = api.EventTypeEnrollmentRequestApproved
	if status.Code == http.StatusOK {
		event.Message = "Successfully approved"
	} else {
		event.Message = status.Message
	}
	return event
}

func GetResourceDeletedEvent(ctx context.Context, eventStore store.Event, log logrus.FieldLogger, orgId uuid.UUID, status api.Status, resourceName string, ResourceKind api.ResourceKind) *api.Event {
	event := GetCommonEvent(ctx, status, resourceName, ResourceKind)
	if event == nil {
		return nil
	}
	event.Type = api.EventTypeResourceDeleted
	if status.Code == http.StatusOK {
		event.Message = "Successfully deleted"
	} else {
		event.Message = status.Message
	}
	return event
}

func GetResourceUpdatedEvent(ctx context.Context, eventStore store.Event, log logrus.FieldLogger, orgId uuid.UUID, status api.Status, resourceName string, ResourceKind api.ResourceKind, details *api.ResourceUpdatedDetails) *api.Event {
	event := GetCommonEvent(ctx, status, resourceName, ResourceKind)
	if event == nil {
		return nil
	}

	if status.Code == http.StatusOK {
		fields := ""
		if details != nil && len(details.UpdatedFields) > 0 {
			stringFields := make([]string, len(details.UpdatedFields))
			for i, field := range details.UpdatedFields {
				stringFields[i] = string(field)
			}
			fields = fmt.Sprintf(" (including %s)", strings.Join(stringFields, ","))
		}
		event.Message = fmt.Sprintf("Successfully updated%s", fields)
	} else if status.Code == http.StatusCreated {
		event.Message = "Successfully created"
	} else {
		event.Message = status.Message
	}

	event.Type = api.EventTypeResourceCreated
	if details != nil && status.Code != http.StatusCreated {
		event.Type = api.EventTypeResourceUpdated
		event.Details = &api.EventDetails{}
		err := event.Details.FromResourceUpdatedDetails(*details)
		if err != nil {
			log.Errorf("failed emitting resource updated event for %s %s/%s: %v", ResourceKind, orgId, resourceName, err)
			return nil
		}
	}

	return nil
}
