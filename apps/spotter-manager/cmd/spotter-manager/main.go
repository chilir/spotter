// apps/spotter-manager/cmd/spotter-manager/main.go

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ServeFrontend)
	mux.HandleFunc("/deploy", DeployHandler)
	mux.HandleFunc("/delete", DeleteHandler)
	mux.HandleFunc("/detect", DetectProxyHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Println("Starting spotter-manager server on port 8080...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on %s: %v\n", server.Addr, err)
		}
	}()
	log.Println("spotter-manager server started successfully")

	<-stop
	log.Println("Shutting down spotter-manager server...")

	// Give the server 5 seconds to shutdown gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("spotter-manager server shutdown failed:%+v", err)
	}

	log.Println("spotter-manager server shutdown successfully")
}
