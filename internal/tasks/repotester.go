package tasks

import (
	"context"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	commonauth "github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/pkg/reqid"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/sirupsen/logrus"
)

type API interface {
	Test()
}

type RepoTester struct {
	log                    logrus.FieldLogger
	serviceHandler         *service.ServiceHandler
	TypeSpecificRepoTester TypeSpecificRepoTester
}

func NewRepoTester(log logrus.FieldLogger, serviceHandler *service.ServiceHandler) *RepoTester {
	return &RepoTester{
		log:            log,
		serviceHandler: serviceHandler,
	}
}

func (r *RepoTester) TestRepositories() {
	reqid.OverridePrefix("repotester")
	requestID := reqid.NextRequestID()
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, requestID)
	ctx = context.WithValue(ctx, commonauth.InternalRequestCtxKey, true)
	log := log.WithReqIDFromCtx(ctx, r.log)

	log.Info("Running RepoTester")

	// TODO: Once we implement multi-tenancy (orgId), the code will need to change to work on all orgs
	repositories, err := listRepositories(ctx, r.serviceHandler, api.ListRepositoriesParams{})
	if err != nil {
		log.Errorf("%v", err)
		return
	}

	for i := range repositories.Items {
		repository := repositories.Items[i]

		repoSpec, _ := repository.Spec.GetGenericRepoSpec()
		switch repoSpec.Type {
		case "http":
			log.Info("Detected HTTP repository type")
			r.TypeSpecificRepoTester = &HttpRepoTester{}
		case "git":
			log.Info("Defaulting to Git repository type")
			r.TypeSpecificRepoTester = &GitRepoTester{}
		default:
			log.Errorf("unsupported repository type: %s", repoSpec.Type)
		}

		accessErr := r.TypeSpecificRepoTester.TestAccess(&repository)

		err := r.SetAccessCondition(ctx, &repository, accessErr)
		if err != nil {
			log.Errorf("Failed to update repository status for %s: %v", *repository.Metadata.Name, err)
		}
	}
}

type TypeSpecificRepoTester interface {
	TestAccess(repository *api.Repository) error
}

type GitRepoTester struct {
}

type HttpRepoTester struct {
}

func (r *GitRepoTester) TestAccess(repository *api.Repository) error {
	repoURL, err := repository.Spec.GetRepoURL()
	if err != nil {
		return err
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name:  *repository.Metadata.Name,
		URLs:  []string{repoURL},
		Fetch: []config.RefSpec{"HEAD"},
	})

	listOps := &git.ListOptions{}
	auth, err := GetAuth(repository)
	if err != nil {
		return err
	}

	listOps.Auth = auth
	_, err = remote.List(listOps)
	return err
}

func (r *HttpRepoTester) TestAccess(repository *api.Repository) error {
	repoHttpSpec, err := repository.Spec.GetHttpRepoSpec()
	if err != nil {
		return fmt.Errorf("failed to get HTTP repo spec: %w", err)
	}

	repoURL := repoHttpSpec.Url
	// Append the validationSuffix if it exists
	if repoHttpSpec.ValidationSuffix != nil {
		repoURL += *repoHttpSpec.ValidationSuffix
	}

	repoSpec := repository.Spec
	_, err = sendHTTPrequest(repoSpec, repoURL)
	return err
}

func (r *RepoTester) SetAccessCondition(ctx context.Context, repository *api.Repository, err error) error {
	condition := api.Condition{Type: api.RepositoryAccessible}
	if err == nil {
		condition.Status = api.ConditionStatusTrue
		condition.Reason = "Accessible"
		condition.Message = "Accessible"
	} else {
		condition.Status = api.ConditionStatusFalse
		condition.Reason = "Inaccessible"
		condition.Message = err.Error()
	}
	return r.serviceHandler.UpdateRepositoryConditions(ctx, *repository.Metadata.Name, []api.Condition{condition})
}
