package main

import (
	"github.com/flightctl/flightctl/internal/alert_exporter"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/sirupsen/logrus"
)

func main() {
	log := log.InitLogs()
	log.Println("Starting alert exporter")

	cfg, err := config.LoadOrGenerate(config.ConfigFile())
	if err != nil {
		log.Fatalf("reading configuration: %v", err)
	}
	log.Printf("Using config: %s", cfg)

	logLvl, err := logrus.ParseLevel(cfg.Service.LogLevel)
	if err != nil {
		logLvl = logrus.InfoLevel
	}
	log.SetLevel(logLvl)

	server := alert_exporter.New(cfg, log)
	if err := server.Run(); err != nil {
		log.Fatalf("Error running server: %s", err)
	}
}
