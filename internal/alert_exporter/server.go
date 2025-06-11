package alert_exporter

import (
	"context"
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

	alertExporter := NewAlertExporter(s.log, serviceHandler, s.cfg)

	// Signal handling

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		s.log.Infof("Received signal %s, shutting down", sig)
		cancel()
	}()

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			s.log.Info("Context canceled, exiting alert exporter")
			return nil
		}

		err := alertExporter.Poll(ctx) // This runs its own ticker with s.interval
		if err != nil {
			s.log.Errorf("Poller failed: %v. Restarting after %v...", err, backoff)
			select {
			case <-time.After(backoff):
				backoff *= 2
				if backoff > 60*time.Second {
					backoff = 60 * time.Second
				}
			case <-ctx.Done():
				s.log.Info("Context cancelled during backoff, exiting")
				return nil
			}
		} else {
			backoff = time.Second // Reset if Poll exited cleanly (unusual)
		}
	}
}
