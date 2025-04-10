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

	"spotter-manager/internal/handlers"
)

func main() {
	// init  k8s client
	k8sClient, err := handlers.SetupKubernetesClient()
	if err != nil {
		log.Fatalf("Kubernetes client initialization failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.ServeFrontend)
	mux.HandleFunc("/deploy", handlers.MakeDeployHandler(k8sClient))
	mux.HandleFunc("/delete", handlers.MakeDeleteHandler(k8sClient))
	mux.HandleFunc("/detect", handlers.DetectProxyHandler)

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
