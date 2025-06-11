package alert_exporter

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/tasks_client"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/sirupsen/logrus"
)

type Server struct {
	cfg *config.Config
	log *logrus.Logger
}

// New returns a new instance of a flightctl server.
func New(
	cfg *config.Config,
	log *logrus.Logger,
) *Server {
	return &Server{
		cfg: cfg,
		log: log,
	}
}

func (s *Server) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, consts.EventSourceComponentCtxKey, "flightctl-alert-exporter")
	ctx = context.WithValue(ctx, consts.EventActorCtxKey, "service:flightctl-alert-exporter")
	defer cancel()

	s.log.Println("Initializing data store")
	db, err := store.InitDB(s.cfg, s.log)
	if err != nil {
		s.log.Fatalf("initializing data store: %v", err)
	}

	store := store.NewStore(db, s.log.WithField("pkg", "store"))
	defer store.Close()

	queuesProvider, err := queues.NewRedisProvider(context.Background(), s.log, s.cfg.KV.Hostname, s.cfg.KV.Port, s.cfg.KV.Password)
	if err != nil {
		return err
	}
	defer queuesProvider.Stop()

	kvStore, err := kvstore.NewKVStore(ctx, s.log, s.cfg.KV.Hostname, s.cfg.KV.Port, s.cfg.KV.Password)
	if err != nil {
		return err
	}

	publisher, err := tasks_client.TaskQueuePublisher(queuesProvider)
	if err != nil {
		return err
	}
	callbackManager := tasks_client.NewCallbackManager(publisher, s.log)
	serviceHandler := service.NewServiceHandler(store, callbackManager, kvStore, nil, s.log, "", "")

	eventPoller := NewEventPoller(s.log, serviceHandler, 30*time.Second)

	// Start polling in background
	go eventPoller.Poll(ctx)

	http.Handle("/metrics", http.HandlerFunc(MetricsHandler))
	srv := &http.Server{
		Addr:         ":8000",
		Handler:      http.DefaultServeMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	err = srv.ListenAndServe()
	if err != nil {
		s.log.Fatalf("failed to start metrics server: %v", err)
	}
	s.log.Info("Metrics server started on :8000/metrics")

	sigShutdown := make(chan os.Signal, 1)

	signal.Notify(sigShutdown, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	<-sigShutdown
	s.log.Println("Shutdown signal received")
	return nil
}
