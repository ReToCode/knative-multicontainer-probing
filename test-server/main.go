package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

var isLive = true
var execOk = true
var isReady = false

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// if called with "exec" arg, call the server to check for exec health
	if len(os.Args) > 1 && os.Args[1] == "exec" {
		res, err := http.Get(fmt.Sprintf("http://localhost:%s/exec", port))
		if err != nil {
			log.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			log.Fatalf("got status: %v", res.Status)
		}
		log.Printf("check ok")
		os.Exit(0)
	}

	// make startup a bit slow
	time.Sleep(5 * time.Second)

	// if we are not called with "exec", start the server
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/live", liveHandler)
	http.HandleFunc("/ready", readyHandler)
	http.HandleFunc("/exec", execHandler)
	http.HandleFunc("/toggleLive", toggleLiveHandler)
	http.HandleFunc("/toggleReady", toggleReadyHandler)
	http.HandleFunc("/toggleExec", toggleExecHandler)
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

func toggleExecHandler(writer http.ResponseWriter, request *http.Request) {
	execOk = !execOk
	fmt.Println("Exec is now: ", execOk)
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

func execHandler(writer http.ResponseWriter, request *http.Request) {
	fmt.Println("Exec probe called, responding with: ", execOk)
	if execOk {
		writer.WriteHeader(http.StatusOK)
	} else {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
}
