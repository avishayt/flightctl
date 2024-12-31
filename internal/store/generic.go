package store

import (
	"context"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Resource represents any API resource that can be stored
type GenericResource interface {
	GetKind() string
	GetName() string
	GetOrgID() uuid.UUID
	SetOrgID(orgId uuid.UUID)
	GetResourceVersion() *int64
	SetResourceVersion(version *int64)
	GetGeneration() *int64
	SetGeneration(generation *int64)
	GetOwner() *string
	SetOwner(owner *string)
	GetLabels() pq.StringArray
	SetLabels(annotations pq.StringArray)
	GetAnnotations() pq.StringArray
	SetAnnotations(annotations pq.StringArray)
	HasSameSpecAs(otherResource any) bool
}

type IntegrationTestCallback func()

// GenericStore provides generic CRUD operations for resources
type GenericStore[M GenericResource, A any] struct {
	db  *gorm.DB
	log logrus.FieldLogger

	// Conversion functions between API and model types
	toModel func(api *A) (M, error)
	toAPI   func(model M) A

	// Callback for integration tests to inject logic
	IntegrationTestCreateOrUpdateCallback IntegrationTestCallback
}

func NewGenericStore[M GenericResource, A any](
	db *gorm.DB,
	log logrus.FieldLogger,
	toModel func(*A) (M, error),
	toAPI func(M) A,
) *GenericStore[M, A] {
	return &GenericStore[M, A]{
		db:                                    db,
		log:                                   log,
		toModel:                               toModel,
		toAPI:                                 toAPI,
		IntegrationTestCreateOrUpdateCallback: func() {},
	}
}

func (s *GenericStore[M, A]) Create(ctx context.Context, orgId uuid.UUID, resource *A, callback func(before, after M)) (*A, error) {
	updated, _, _, err := s.createOrUpdate(orgId, resource, nil, true, ModeCreateOnly, callback)
	return updated, err
}

func (s *GenericStore[M, A]) Update(ctx context.Context, orgId uuid.UUID, resource *A, fieldsToUnset []string, fromAPI bool, callback func(before, after M)) (*A, error) {
	updated, _, err := retryCreateOrUpdate(func() (*A, bool, bool, error) {
		return s.createOrUpdate(orgId, resource, fieldsToUnset, fromAPI, ModeUpdateOnly, callback)
	})
	return updated, err
}

func (s *GenericStore[M, A]) CreateOrUpdate(ctx context.Context, orgId uuid.UUID, resource *A, fieldsToUnset []string, fromAPI bool, callback func(before, after M)) (*A, bool, error) {
	return retryCreateOrUpdate(func() (*A, bool, bool, error) {
		return s.createOrUpdate(orgId, resource, fieldsToUnset, fromAPI, ModeCreateOrUpdate, callback)
	})
}

func (s *GenericStore[M, A]) createOrUpdate(orgId uuid.UUID, resource *A, fieldsToUnset []string, fromAPI bool, mode CreateOrUpdateMode, callback func(before, after M)) (*A, bool, bool, error) {
	if resource == nil {
		return nil, false, false, flterrors.ErrResourceIsNil
	}

	model, err := s.toModel(resource)
	if err != nil {
		return nil, false, false, err
	}
	model.SetOrgID(orgId)
	model.SetAnnotations(nil)

	existing, err := getExistingRecord[M](s.db, model.GetName(), orgId)
	if err != nil {
		return nil, false, false, err
	}
	exists := existing != nil

	if exists && mode == ModeCreateOnly {
		return nil, false, false, flterrors.ErrDuplicateName
	}
	if !exists && mode == ModeUpdateOnly {
		return nil, false, false, flterrors.ErrResourceNotFound
	}

	s.IntegrationTestCreateOrUpdateCallback()

	var retry bool
	if !exists {
		retry, err = s.createResource(model)
	} else {
		retry, err = s.updateResource(fromAPI, *existing, model, fieldsToUnset)
	}
	if err != nil {
		return nil, false, retry, err
	}

	if callback != nil {
		callback(*existing, model)
	}

	apiResource := s.toAPI(model)
	return &apiResource, !exists, false, nil
}

func (s *GenericStore[M, A]) createResource(resource M) (bool, error) {
	resource.SetGeneration(util.Int64ToPtr(1))
	resource.SetResourceVersion(util.Int64ToPtr(1))

	if result := s.db.Create(resource); result.Error != nil {
		err := ErrorFromGormError(result.Error)
		return err == flterrors.ErrDuplicateName, err
	}
	return false, nil
}

func (s *GenericStore[M, A]) updateResource(fromAPI bool, existing, resource M, fieldsToUnset []string) (bool, error) {
	sameSpec := resource.HasSameSpecAs(existing)

	if !sameSpec {
		if fromAPI {
			if len(lo.FromPtr(existing.GetOwner())) != 0 {
				// Don't let the user update the spec if it has an owner
				return false, flterrors.ErrUpdatingResourceWithOwnerNotAllowed
			} else {
				// Remove the TemplateVersion annotation if the device has no owner
				if resource.GetKind() == api.DeviceKind {
					existingAnnotations := util.LabelArrayToMap(existing.GetAnnotations())
					if existingAnnotations[api.DeviceAnnotationTemplateVersion] != "" {
						delete(existingAnnotations, api.DeviceAnnotationTemplateVersion)
						annotationsArray := util.LabelMapToArray(&existingAnnotations)
						resource.SetAnnotations(pq.StringArray(annotationsArray))
					}
				}
			}
		}

		// Update the generation if the spec was updated
		resource.SetGeneration(lo.ToPtr(lo.FromPtr(existing.GetGeneration()) + 1))
	}

	if resource.GetResourceVersion() != nil &&
		lo.FromPtr(existing.GetResourceVersion()) != lo.FromPtr(resource.GetResourceVersion()) {
		return false, flterrors.ErrResourceVersionConflict
	}

	resource.SetResourceVersion(lo.ToPtr(lo.FromPtr(existing.GetResourceVersion()) + 1))

	selectFields := []string{"spec"}
	if resource.GetKind() == api.DeviceKind {
		selectFields = append(selectFields, "alias")
	}
	selectFields = append(selectFields, s.getNonNilFieldsFromResource(resource)...)
	selectFields = append(selectFields, fieldsToUnset...)

	query := s.db.Model(resource).
		Where("org_id = ? AND name = ? AND resource_version = ?",
			resource.GetOrgID(),
			resource.GetName(),
			lo.FromPtr(existing.GetResourceVersion())).
		Select(selectFields)

	result := query.Updates(resource)
	if result.Error != nil {
		return false, ErrorFromGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return true, flterrors.ErrNoRowsUpdated
	}
	return false, nil
}

func (s *GenericStore[M, A]) getNonNilFieldsFromResource(resource M) []string {
	ret := []string{}
	if resource.GetGeneration() != nil {
		ret = append(ret, "generation")
	}
	if resource.GetLabels() != nil {
		ret = append(ret, "labels")
	}
	if resource.GetOwner() != nil {
		ret = append(ret, "owner")
	}
	if resource.GetAnnotations() != nil {
		ret = append(ret, "annotations")
	}

	if resource.GetGeneration() != nil {
		ret = append(ret, "generation")
	}

	if resource.GetResourceVersion() != nil {
		ret = append(ret, "resource_version")
	}

	return ret
}

/*
func (s *GenericStore[M, A]) List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.ResourceList[A], error) {
	var resources []M
	var nextContinue *string
	var numRemaining *int64

	if listParams.Limit < 0 {
		return nil, flterrors.ErrLimitParamOutOfBounds
	}

	query, err := ListQuery(new(M)).Build(ctx, s.db, orgId, listParams)
	if err != nil {
		return nil, err
	}

	if listParams.Limit > 0 {
		query = AddPaginationToQuery(query, listParams.Limit+1, listParams.Continue)
	}

	result := query.Find(&resources)
	if result.Error != nil {
		return nil, ErrorFromGormError(result.Error)
	}

	if listParams.Limit > 0 && len(resources) > listParams.Limit {
		nextContinueStruct := Continue{
			Name:    resources[len(resources)-1].GetName(),
			Version: CurrentContinueVersion,
		}
		resources = resources[:len(resources)-1]

		var numRemainingVal int64
		if listParams.Continue != nil {
			numRemainingVal = listParams.Continue.Count - int64(listParams.Limit)
			if numRemainingVal < 1 {
				numRemainingVal = 1
			}
		} else {
			countQuery, err := ListQuery(new(M)).Build(ctx, s.db, orgId, listParams)
			if err != nil {
				return nil, err
			}
			numRemainingVal = CountRemainingItems(countQuery, nextContinueStruct.Name)
		}
		nextContinueStruct.Count = numRemainingVal
		contByte, _ := json.Marshal(nextContinueStruct)
		contStr := b64.StdEncoding.EncodeToString(contByte)
		nextContinue = &contStr
		numRemaining = &numRemainingVal
	}

	items := make([]A, len(resources))
	for i, r := range resources {
		items[i] = s.toAPI(r)
	}

	return &api.ResourceList[A]{
		Items:     items,
		Continue:  nextContinue,
		Remaining: numRemaining,
	}, nil
}

func (s *GenericStore[M, A]) Get(ctx context.Context, orgId uuid.UUID, name string) (*A, error) {
	var resource M
	result := s.db.Where("org_id = ? AND name = ?", orgId, name).First(&resource)
	if result.Error != nil {
		return nil, ErrorFromGormError(result.Error)
	}

	apiResource := s.toAPI(resource)
	return &apiResource, nil
}

func (s *GenericStore[M, A]) Delete(ctx context.Context, orgId uuid.UUID, name string) error {
	var existingRecord M
	result := s.db.Where("org_id = ? AND name = ?", orgId, name).First(&existingRecord)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil
		}
		return ErrorFromGormError(result.Error)
	}

	if err := s.db.Delete(&existingRecord).Error; err != nil {
		return ErrorFromGormError(err)
	}

	if s.resourceCallback != nil {
		s.resourceCallback(&existingRecord, nil)
	}

	return nil
}
*/
