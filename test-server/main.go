package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

var isHealthy = true

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/live", liveHandler)
	http.HandleFunc("/ready", readyHandler)
	http.HandleFunc("/toggle", toggleHandler)
	srv := &http.Server{
		Addr:         ":" + port,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	fmt.Println("Starting server. Listening on port: ", port)
	log.Fatal(srv.ListenAndServe())
}

func toggleHandler(writer http.ResponseWriter, request *http.Request) {
	isHealthy = !isHealthy
	fmt.Println("Healthy is now set to: ", isHealthy)
	writer.WriteHeader(http.StatusOK)
}

func rootHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("/ called")
	writer.WriteHeader(http.StatusOK)
}

func readyHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("Readiness probe called")
	writer.WriteHeader(http.StatusOK)
}

func liveHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("Liveness probe called, responding with: ", isHealthy)
	if isHealthy {
		writer.WriteHeader(http.StatusOK)
	} else {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
}
