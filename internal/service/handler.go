package service

import (
	"github.com/flightctl/flightctl/internal/crypto"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/sirupsen/logrus"
)

type ServiceHandler struct {
	*EventHandler
	store         store.Store
	ca            *crypto.CAClient
	log           logrus.FieldLogger
	kvStore       kvstore.KVStore
	agentEndpoint string
	uiUrl         string
}

func NewServiceHandler(store store.Store, kvStore kvstore.KVStore, ca *crypto.CAClient, log logrus.FieldLogger, agentEndpoint string, uiUrl string) *ServiceHandler {
	return &ServiceHandler{
		EventHandler:  NewEventHandler(store, log),
		store:         store,
		ca:            ca,
		log:           log,
		kvStore:       kvStore,
		agentEndpoint: agentEndpoint,
		uiUrl:         uiUrl,
	}
}
