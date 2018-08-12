package main

import (
	"log"
	"strconv"
	"os"

	"github.com/minienv/minienv-api-core"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	if _, err := strconv.Atoi(os.Args[1]); err != nil {
		log.Fatalf("Invalid port: %s (%s)\n", os.Args[1], err)
	}
	log.Printf("Starting API Server with no AuthProvider...")
	apiServer := minienv.ApiServer{
		Port: os.Args[1],
		AuthProvider: nil,
	}
	apiServer.Run()
}
