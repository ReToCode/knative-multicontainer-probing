package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

var isLive = true
var isReady = false

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/live", liveHandler)
	http.HandleFunc("/ready", readyHandler)
	http.HandleFunc("/toggleLive", toggleLiveHandler)
	http.HandleFunc("/toggleReady", toggleReadyHandler)
	srv := &http.Server{
		Addr:         ":" + port,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	fmt.Println("Starting server. Listening on port: ", port)
	log.Fatal(srv.ListenAndServe())
}

func toggleLiveHandler(writer http.ResponseWriter, request *http.Request) {
	isLive = !isLive
	fmt.Println("Liveness is now: ", isLive)
	writer.WriteHeader(http.StatusOK)
}

func toggleReadyHandler(writer http.ResponseWriter, request *http.Request) {
	isReady = !isReady
	fmt.Println("Readiness is now: ", isLive)
	writer.WriteHeader(http.StatusOK)
}

func rootHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("/ called")
	writer.WriteHeader(http.StatusOK)
}

func readyHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("Readiness probe called, responding with: ", isReady)

	if isReady {
		writer.WriteHeader(http.StatusOK)
	} else {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
}

func liveHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("Liveness probe called, responding with: ", isLive)
	if isLive {
		writer.WriteHeader(http.StatusOK)
	} else {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
}
