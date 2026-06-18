package main

import (
	"log"

	delivery "den-services/delivery/internal"
)

func main() {
	cfg, err := delivery.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}
	server, err := delivery.NewHTTPServer(cfg)
	if err != nil {
		log.Fatalf("building server: %v", err)
	}
	log.Printf("delivery intent-claim listening on %s", cfg.BindAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("delivery intent-claim server: %v", err)
	}
}
