package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	commonauth "github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/service/common"
	"github.com/flightctl/flightctl/internal/tasks_client"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/pkg/reqid"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-git/go-billy/v5"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

type ResourceSync struct {
	log             logrus.FieldLogger
	serviceHandler  *service.ServiceHandler
	callbackManager tasks_client.CallbackManager
}

type genericResourceMap map[string]interface{}

var validFileExtensions = []string{"json", "yaml", "yml"}
var supportedResources = []string{api.FleetKind}

func NewResourceSync(callbackManager tasks_client.CallbackManager, serviceHandler *service.ServiceHandler, log logrus.FieldLogger) *ResourceSync {
	return &ResourceSync{
		log:             log,
		serviceHandler:  serviceHandler,
		callbackManager: callbackManager,
	}
}

func (r *ResourceSync) Poll() {
	reqid.OverridePrefix("resourcesync")
	requestID := reqid.NextRequestID()
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, requestID)
	ctx = context.WithValue(ctx, commonauth.InternalRequestCtxKey, true)
	log := log.WithReqIDFromCtx(ctx, r.log)

	log.Info("Running ResourceSync Polling")

	resourcesyncs, err := listResourceSyncs(ctx, r.serviceHandler, api.ListResourceSyncsParams{})
	if err != nil {
		log.Errorf("error fetching resourcesyncs: %s", err)
		return
	}

	for i := range resourcesyncs.Items {
		rs := &resourcesyncs.Items[i]
		_ = r.run(ctx, log, rs)
	}
}

func (r *ResourceSync) run(ctx context.Context, log logrus.FieldLogger, rs *api.ResourceSync) error {
	defer r.updateResourceSyncStatus(ctx, rs)
	reponame := rs.Spec.Repository
	repo, err := getRepository(ctx, r.serviceHandler, reponame)
	if err != nil {
		// Failed to fetch Repository resource
		addRepoNotFoundCondition(rs, err)
		return err
	}
	addRepoNotFoundCondition(rs, nil)
	resources, err := r.parseAndValidateResources(rs, repo, CloneGitRepo)
	if err != nil {
		log.Errorf("resourcesync/%s: parsing failed. error: %s", *rs.Metadata.Name, err.Error())
		return err
	}
	if resources == nil {
		// No resources to sync
		return nil
	}

	owner := util.SetResourceOwner(api.ResourceSyncKind, *rs.Metadata.Name)
	fleets, err := r.parseFleets(resources, owner)
	if err != nil {
		err := fmt.Errorf("resourcesync/%s: error: %w", *rs.Metadata.Name, err)
		log.Errorf("%e", err)
		addResourceParsedCondition(rs, err)
		return err
	}
	addResourceParsedCondition(rs, nil)

	fleetsPreOwned := make([]api.Fleet, 0)

	listParams := api.ListFleetsParams{
		Limit:         lo.ToPtr(int32(ItemsPerPage)),
		FieldSelector: lo.ToPtr(fmt.Sprintf("metadata.owner=%s", *owner)),
	}
	for {
		listRes, err := listFleets(ctx, r.serviceHandler, listParams)
		if err != nil {
			err := fmt.Errorf("resourcesync/%s: failed to list owned fleets. error: %w", *rs.Metadata.Name, err)
			log.Errorf("%e", err)
			return err
		}
		fleetsPreOwned = append(fleetsPreOwned, listRes.Items...)
		if listRes.Metadata.Continue == nil {
			break
		}
		if listRes.Metadata.Continue == nil {
			break
		}
		listParams.Continue = listRes.Metadata.Continue
	}

	fleetsToRemove := fleetsDelta(fleetsPreOwned, fleets)

	r.log.Infof("resourcesync/%s: applying #%d fleets ", *rs.Metadata.Name, len(fleets))
	err = r.createOrUpdateMultiple(ctx, fleets...)
	if err == flterrors.ErrUpdatingResourceWithOwnerNotAllowed {
		err = fmt.Errorf("one or more fleets are managed by a different resource. %w", err)
	}
	if len(fleetsToRemove) > 0 {
		r.log.Infof("resourcesync/%s: found #%d fleets to remove. removing\n", *rs.Metadata.Name, len(fleetsToRemove))
		for _, fleetToRemove := range fleetsToRemove {
			_, err := deleteFleet(ctx, r.serviceHandler, fleetToRemove)
			if err != nil {
				log.Errorf("resourcesync/%s: failed to remove old fleet %s. error: %s", *rs.Metadata.Name, fleetToRemove, err.Error())
				return err
			}
		}
	}
	addSyncedCondition(rs, err)
	if err != nil {
		log.Errorf("resourcesync/%s: failed to apply resource. error: %s", *rs.Metadata.Name, err.Error())
		return err
	}
	rs.Status.ObservedGeneration = rs.Metadata.Generation
	r.log.Infof("resourcesync/%s: #%d fleets applied successfully\n", *rs.Metadata.Name, len(fleets))
	return nil
}

func (r *ResourceSync) createOrUpdateMultiple(ctx context.Context, resources ...*api.Fleet) error {
	var errs []error
	for _, resource := range resources {
		_, err := replaceFleet(ctx, r.serviceHandler, resource)
		if err == flterrors.ErrUpdatingResourceWithOwnerNotAllowed {
			err = fmt.Errorf("one or more fleets are managed by a different resource. %w", err)
		}
		errs = append(errs, err)
	}
	return errors.Join(lo.Uniq(errs)...)
}

// Returns a list of names that are no longer present
func fleetsDelta(owned []api.Fleet, newOwned []*api.Fleet) []string {
	dfleets := make([]string, 0)
	for _, ownedFleet := range owned {
		found := false
		name := ownedFleet.Metadata.Name
		for _, newFleet := range newOwned {
			if *name == *newFleet.Metadata.Name {
				found = true
				break
			}
		}
		if !found {
			dfleets = append(dfleets, *name)
		}
	}

	return dfleets
}

func (r *ResourceSync) parseAndValidateResources(rs *api.ResourceSync, repo *api.Repository, gitCloneRepo cloneGitRepoFunc) ([]genericResourceMap, error) {
	path := rs.Spec.Path
	revision := rs.Spec.TargetRevision
	mfs, hash, err := gitCloneRepo(repo, &revision, lo.ToPtr(1))
	if err != nil {
		// Cant fetch git repo
		addRepoAccessCondition(rs, err)
		return nil, err
	}
	addRepoAccessCondition(rs, nil)

	if !needsSyncToHash(rs, hash) {
		// nothing to update
		r.log.Infof("resourcesync/%s: No new commits or path. skipping", *rs.Metadata.Name)
		return nil, nil
	}
	addSyncedCondition(rs, fmt.Errorf("out of sync"))

	rs.Status.ObservedCommit = lo.ToPtr(hash)

	// Open files
	fileInfo, err := mfs.Stat(path)
	if err != nil {
		// Can't access path
		addPathAccessCondition(rs, err)
		return nil, err
	}
	addPathAccessCondition(rs, nil)
	var resources []genericResourceMap
	if fileInfo.IsDir() {
		resources, err = r.extractResourcesFromDir(mfs, path)
	} else {
		resources, err = r.extractResourcesFromFile(mfs, path)
	}
	if err != nil {
		// Failed to parse resources
		addResourceParsedCondition(rs, err)
		return nil, err

	}
	addResourceParsedCondition(rs, nil)
	return resources, nil
}

func (r *ResourceSync) extractResourcesFromDir(mfs billy.Filesystem, path string) ([]genericResourceMap, error) {
	genericResources := []genericResourceMap{}
	files, err := mfs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && isValidFile(file.Name()) { // Not going recursively into subfolders
			resources, err := r.extractResourcesFromFile(mfs, mfs.Join(path, file.Name()))
			if err != nil {
				return nil, err
			}
			genericResources = append(genericResources, resources...)
		}
	}
	return genericResources, nil
}

func (r *ResourceSync) extractResourcesFromFile(mfs billy.Filesystem, path string) ([]genericResourceMap, error) {
	genericResources := []genericResourceMap{}

	file, err := mfs.Open(path)
	if err != nil {
		// Failed to open file....
		return nil, err
	}
	defer file.Close()
	decoder := yamlutil.NewYAMLOrJSONDecoder(file, 100)

	for {
		var resource genericResourceMap
		err = decoder.Decode(&resource)
		if err != nil {
			break
		}
		kind, kindok := resource["kind"].(string)
		meta, metaok := resource["metadata"].(map[string]interface{})
		if !kindok || !metaok {
			return nil, fmt.Errorf("invalid resource definition at '%s'", path)
		}
		_, nameok := meta["name"].(string)
		if !nameok {
			return nil, fmt.Errorf("invalid resource definition at '%s'. resource name missing", path)
		}
		isSupportedResource := false
		for _, supportedResource := range supportedResources {
			if kind == supportedResource {
				isSupportedResource = true
				break
			}
		}
		if !isSupportedResource {
			return nil, fmt.Errorf("invalid resource type at '%s'. unsupported kind '%s'", path, kind)
		}
		genericResources = append(genericResources, resource)
	}
	if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return genericResources, nil

}

func (r ResourceSync) parseFleets(resources []genericResourceMap, owner *string) ([]*api.Fleet, error) {
	fleets := make([]*api.Fleet, 0)
	names := make(map[string]string)
	for _, resource := range resources {
		kind, ok := resource["kind"].(string)
		if !ok {
			return nil, fmt.Errorf("resource with unspecified kind: %v", resource)
		}
		buf, err := json.Marshal(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to parse generic resource: %w", err)
		}

		switch kind {
		case api.FleetKind:
			var fleet api.Fleet
			err := yamlutil.Unmarshal(buf, &fleet)
			if err != nil {
				return nil, fmt.Errorf("decoding Fleet resource: %w", err)
			}
			if fleet.Metadata.Name == nil {
				return nil, fmt.Errorf("decoding Fleet resource: missing field .metadata.name: %w", err)
			}
			if errs := fleet.Validate(); len(errs) > 0 {
				return nil, fmt.Errorf("failed validating fleet: %w", errors.Join(errs...))
			}
			name, nameExists := names[*fleet.Metadata.Name]
			if nameExists {
				return nil, fmt.Errorf("found multiple fleet definitions with name '%s'", name)
			}
			names[name] = name
			// don't overwrite fields that are managed by the service
			fleet.Status = nil
			common.NilOutManagedObjectMetaProperties(&fleet.Metadata)
			if fleet.Spec.Template.Metadata != nil {
				common.NilOutManagedObjectMetaProperties(fleet.Spec.Template.Metadata)
			}

			fleet.Metadata.Owner = owner

			fleets = append(fleets, &fleet)
		default:
			return nil, fmt.Errorf("resource of unknown/unsupported kind %q: %v", kind, resource)
		}
	}

	return fleets, nil
}

func (r *ResourceSync) updateResourceSyncStatus(ctx context.Context, rs *api.ResourceSync) {
	_, err := r.serviceHandler.ReplaceResourceSyncStatus(ctx, rs)
	if err != nil {
		r.log.Errorf("Failed to update resourcesync status for %s: %v", *rs.Metadata.Name, err)
	}
}

func isValidFile(filename string) bool {
	ext := ""
	splits := strings.Split(filename, ".")
	if len(splits) > 0 {
		ext = splits[len(splits)-1]
	}
	for _, validExt := range validFileExtensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

// needsSyncToHash returns true if the resource needs to be synced to the given hash.
func needsSyncToHash(rs *api.ResourceSync, hash string) bool {
	if rs.Status == nil || rs.Status.Conditions == nil {
		return true
	}

	if api.IsStatusConditionFalse(rs.Status.Conditions, api.ResourceSyncSynced) {
		return true
	}

	var generation int64 = 0
	if rs.Metadata.Generation != nil {
		generation = *rs.Metadata.Generation
	}

	var observedGen int64 = 0
	if rs.Status.ObservedGeneration != nil {
		observedGen = *rs.Status.ObservedGeneration
	}
	var prevHash string = util.DefaultIfNil(rs.Status.ObservedCommit, "")
	return hash != prevHash || observedGen != generation
}

func ensureConditionsNotNil(rs *api.ResourceSync) {
	if rs.Status == nil {
		rs.Status = &api.ResourceSyncStatus{Conditions: []api.Condition{}}
	}
}

func setCondition(rs *api.ResourceSync, conditionType api.ConditionType, okReason, failReason string, err error) bool {
	ensureConditionsNotNil(rs)
	return api.SetStatusConditionByError(&rs.Status.Conditions, conditionType, okReason, failReason, err)
}

func addRepoNotFoundCondition(rs *api.ResourceSync, err error) {
	setCondition(rs, api.ResourceSyncAccessible, "accessible", "repository resource not found", err)
}

func addRepoAccessCondition(rs *api.ResourceSync, err error) {
	setCondition(rs, api.ResourceSyncAccessible, "accessible", "failed to clone repository", err)
}

func addPathAccessCondition(rs *api.ResourceSync, err error) {
	setCondition(rs, api.ResourceSyncAccessible, "accessible", "path not found in repository", err)
}

func addResourceParsedCondition(rs *api.ResourceSync, err error) {
	setCondition(rs, api.ResourceSyncResourceParsed, "Success", "Fail", err)
}

func addSyncedCondition(rs *api.ResourceSync, err error) {
	setCondition(rs, api.ResourceSyncSynced, "Success", "Fail", err)
}
