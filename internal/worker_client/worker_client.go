package worker_client

import (
	"context"
	"encoding/json"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/sirupsen/logrus"
)

type WorkerClient interface {
	EmitEvent(ctx context.Context, event *api.Event)
}

type workerClient struct {
	publisher queues.Publisher
	log       logrus.FieldLogger
}

func QueuePublisher(queuesProvider queues.Provider) (queues.Publisher, error) {
	publisher, err := queuesProvider.NewPublisher(consts.TaskQueue)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}
	return publisher, nil
}

func NewWorkerClient(publisher queues.Publisher, log logrus.FieldLogger) WorkerClient {
	return &workerClient{
		publisher: publisher,
		log:       log,
	}
}

func (t *workerClient) EmitEvent(ctx context.Context, event *api.Event) {
	if event == nil {
		return
	}
	if !shouldEmitEvent(event.Reason) {
		return
	}

	b, err := json.Marshal(event)
	if err != nil {
		t.log.WithError(err).Error("failed to marshal event for workers")
		return
	}
	if err = t.publisher.Publish(ctx, b); err != nil {
		t.log.WithError(err).Error("failed to publish event for workers")
	}
}

// eventReasons contains all event reasons that should be sent to the workers
var eventReasons = map[api.EventReason]struct{}{
	api.EventReasonResourceCreated:             {},
	api.EventReasonResourceUpdated:             {},
	api.EventReasonResourceDeleted:             {},
	api.EventReasonFleetRolloutStarted:         {},
	api.EventReasonReferencedRepositoryUpdated: {},
}

func shouldEmitEvent(reason api.EventReason) bool {
	if _, contains := eventReasons[reason]; contains {
		return true
	}
	return false
}
