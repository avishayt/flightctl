package service

import (
	"github.com/flightctl/flightctl/internal/crypto"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/worker_client"
	"github.com/sirupsen/logrus"
)

type ServiceHandler struct {
	*EventHandler
	store         store.Store
	workerClient  worker_client.WorkerClient
	ca            *crypto.CAClient
	log           logrus.FieldLogger
	kvStore       kvstore.KVStore
	agentEndpoint string
	uiUrl         string
}

func NewServiceHandler(store store.Store, workerClient worker_client.WorkerClient, kvStore kvstore.KVStore, ca *crypto.CAClient, log logrus.FieldLogger, agentEndpoint string, uiUrl string) *ServiceHandler {
	return &ServiceHandler{
		EventHandler:  NewEventHandler(store, log, workerClient),
		store:         store,
		workerClient:  workerClient,
		ca:            ca,
		log:           log,
		kvStore:       kvStore,
		agentEndpoint: agentEndpoint,
		uiUrl:         uiUrl,
	}
}
