// apps/spotter-manager/cmd/spotter-manager/main.go

package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", ServeFrontend)
	http.HandleFunc("/deploy", DeployHandler)
	http.HandleFunc("/delete", DeleteHandler)
	http.HandleFunc("/detect", DetectProxyHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
