package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/instrumentation/tracing"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/pkg/k8sclient"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

func dispatchTasks(serviceHandler service.Service, k8sClient k8sclient.K8SClient, kvStore kvstore.KVStore) queues.ConsumeHandler {
	return func(ctx context.Context, payload []byte, log logrus.FieldLogger) error {
		var event api.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			log.WithError(err).Error("failed to unmarshal consume payload")
			return err
		}

		ctx, span := tracing.StartSpan(ctx, "flightctl/worker", fmt.Sprintf("%s-%s", event.InvolvedObject.Kind, event.Reason))
		defer span.End()

		span.SetAttributes(
			attribute.String("event.kind", event.InvolvedObject.Kind),
			attribute.String("event.name", event.InvolvedObject.Name),
			attribute.String("event.reason", string(event.Reason)),
		)

		log.Infof("reconciling %s, reason %s, kind %s, name %s",
			event.InvolvedObject.Kind, event.Reason, event.InvolvedObject.Name)

		var err error
		var taskName string

		if shouldRolloutFleet(ctx, event, log) {
			taskName = "fleetRollout"
			err = fleetRollout(ctx, event, serviceHandler, log)
		} else if shouldReconcileDeviceOwnership(ctx, event, log) {
			taskName = "fleetSelectorMatching"
			err = fleetSelectorMatching(ctx, event, serviceHandler, log)
		} else if shouldValidateFleet(ctx, event, log) {
			taskName = "fleetValidation"
			err = fleetValidate(ctx, event, serviceHandler, k8sClient, log)
		} else if shouldRenderDevice(ctx, event, log) {
			taskName = "deviceRender"
			err = deviceRender(ctx, event, serviceHandler, k8sClient, kvStore, log)
		} else if shouldUpdateRepositoryReferers(ctx, event, log) {
			taskName = "repositoryUpdate"
			err = repositoryUpdate(ctx, event, serviceHandler, log)
		}

		// Emit InternalTaskFailedEvent for any unhandled task failures
		// This serves as a safety net while preserving specific error handling within tasks
		if err != nil {
			log.WithError(err).Errorf("task %s failed", taskName)

			originalEventJson, err := json.Marshal(event)
			if err != nil {
				log.WithError(err).Error("failed to marshal original event")
				return err
			}

			// Create the event using api package
			event := api.GetBaseEvent(ctx, api.ResourceKind(event.InvolvedObject.Kind), event.InvolvedObject.Name, api.EventReasonInternalTaskFailed,
				fmt.Sprintf("%s internal task failed: %s - %s.", api.ResourceKind(event.InvolvedObject.Kind), taskName, err.Error()), nil)

			details := api.EventDetails{}
			if err := details.FromInternalTaskFailedDetails(api.InternalTaskFailedDetails{
				TaskType:          taskName,
				ErrorMessage:      err.Error(),
				RetryCount:        nil,
				OriginalEventJson: lo.ToPtr(string(originalEventJson)),
			}); err == nil {
				event.Details = &details
			}

			// Emit the event
			serviceHandler.CreateEvent(ctx, event)
		}
		return nil
	}
}

func shouldRolloutFleet(ctx context.Context, event api.Event, log logrus.FieldLogger) bool {
	// If a devices's owner or labels were updated, and the delayDeviceRender annotation is set, return true
	if event.Reason != api.EventReasonResourceUpdated && event.InvolvedObject.Kind == api.DeviceKind {
		if event.Metadata.Annotations != nil {
			if _, ok := (*event.Metadata.Annotations)[api.EventAnnotationDelayDeviceRender]; ok {
				return false
			}
		}
		if hasUpdatedFields(event.Details, log, api.Owner, api.Labels) {
			return true
		}
		return false
	}

	// If we got a rollout started event and it's immediate, return true
	if event.Reason == api.EventReasonFleetRolloutStarted && event.Details != nil {
		details, err := event.Details.AsFleetRolloutStartedDetails()
		if err != nil {
			log.WithError(err).Error("failed to convert event details to fleet rollout started details")
			return false
		}
		return details.RolloutStrategy == api.None
	}

	// TODO: Handle FleetRolloutSelectionUpdated

	return false
}

func shouldReconcileDeviceOwnership(ctx context.Context, event api.Event, log logrus.FieldLogger) bool {
	// If a fleet's label selector was updated, return true
	if event.Reason != api.EventReasonResourceUpdated && event.InvolvedObject.Kind == api.FleetKind {
		if hasUpdatedFields(event.Details, log, api.SpecSelector) {
			return true
		}
		return false
	}

	// If a device's labels were updated, return true
	if event.Reason != api.EventReasonResourceUpdated && event.InvolvedObject.Kind == api.DeviceKind {
		if hasUpdatedFields(event.Details, log, api.Labels) {
			return true
		}
		return false
	}

	return false
}

func shouldValidateFleet(ctx context.Context, event api.Event, log logrus.FieldLogger) bool {
	// If a fleet's template was updated, return true
	if event.Reason != api.EventReasonResourceUpdated && event.InvolvedObject.Kind == api.FleetKind {
		if hasUpdatedFields(event.Details, log, api.SpecTemplate) {
			return true
		}
		return false
	}

	// If a repository that the fleet is associated with was updated, return true
	if event.Reason == api.EventReasonReferencedRepositoryUpdated && event.InvolvedObject.Kind == api.FleetKind {
		return true
	}

	return false
}

func shouldRenderDevice(ctx context.Context, event api.Event, log logrus.FieldLogger) bool {
	// If a repository that the device is associated with was updated, return true
	if event.Reason == api.EventReasonReferencedRepositoryUpdated && event.InvolvedObject.Kind == api.DeviceKind {
		return true
	}

	// TODO: If a device is ready to be rendered due to disruption budget, return true

	return false
}

func shouldUpdateRepositoryReferers(ctx context.Context, event api.Event, log logrus.FieldLogger) bool {
	// If a repository was updated, return true
	if event.Reason != api.EventReasonResourceUpdated && event.InvolvedObject.Kind == api.RepositoryKind {
		if hasUpdatedFields(event.Details, log, api.SpecTemplate) {
			return true
		}
		return false
	}
	return false
}

func hasUpdatedFields(details *api.EventDetails, log logrus.FieldLogger, fields ...api.ResourceUpdatedDetailsUpdatedFields) bool {
	if details == nil {
		return false
	}

	updateDetails, err := details.AsResourceUpdatedDetails()
	if err != nil {
		log.WithError(err).Error("failed to convert event details to resource updated details")
		return false
	}

	updatedFields := updateDetails.UpdatedFields
	for _, field := range updatedFields {
		if lo.Contains(fields, field) {
			return true
		}
	}
	return false
}

func LaunchConsumers(ctx context.Context,
	queuesProvider queues.Provider,
	serviceHandler service.Service,
	k8sClient k8sclient.K8SClient,
	kvStore kvstore.KVStore,
	numConsumers, threadsPerConsumer int) error {
	for i := 0; i != numConsumers; i++ {
		consumer, err := queuesProvider.NewConsumer(consts.TaskQueue)
		if err != nil {
			return err
		}
		for j := 0; j != threadsPerConsumer; j++ {
			if err = consumer.Consume(ctx, dispatchTasks(serviceHandler, k8sClient, kvStore)); err != nil {
				return err
			}
		}
	}
	return nil
}
