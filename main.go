package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"os"

	"github.com/minienv/minienv-api-core"
)

var apiServer *minienv.ApiServer

type SessionHandlerFunc func(http.ResponseWriter, *http.Request, *minienv.Session)

type MeResponse struct {
	SessionId string `json:"sessionId"`
}

func getSessionThenServe(handler SessionHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionId := r.Header.Get("Minienv-Session-Id")
		if sessionId == "" {
			http.Error(w, "No session", 401)
			return
		}
		session, _ := apiServer.GetSession(sessionId)
		if session == nil {
			http.Error(w, "No session", 401)
			return
		}
		handler(w, r, session)
	}
}

func root(w http.ResponseWriter, r *http.Request) {
}

func me(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid me request", 400)
		return
	}
	sessionId := r.Header.Get("Minienv-Session-Id")
	session := apiServer.GetOrCreateSession(sessionId)
	meWithSession(w, r, session)
}

func meWithSession(w http.ResponseWriter, r *http.Request, session *minienv.Session) {
	meResponse := MeResponse{
		SessionId: session.Id,
	}
	err := json.NewEncoder(w).Encode(&meResponse)
	if err != nil {
		log.Println("Error encoding me response: ", err)
		http.Error(w, err.Error(), 400)
	}
}

func claim(w http.ResponseWriter, r *http.Request, _ *minienv.Session) {
	if r.Method != "POST" {
		http.Error(w, "Invalid claim request", 400)
	}
	if r.Body == nil {
		log.Println("Invalid claim request; Body is nil.")
		http.Error(w, "Invalid claim request", 400)
		return
	}
	// decode request
	var claimRequest minienv.ClaimRequest
	err := json.NewDecoder(r.Body).Decode(&claimRequest)
	if err != nil {
		log.Println("Error decoding claim request: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	// encode response
	claimResponse := apiServer.Claim(&claimRequest)
	err = json.NewEncoder(w).Encode(&claimResponse)
	if err != nil {
		log.Println("Error encoding claim response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func whitelist(w http.ResponseWriter, r *http.Request, _ *minienv.Session) {
	if r.Method != "GET" {
		http.Error(w, "Invalid whitelist request", 400)
	}
	var whitelistResponse = apiServer.Whitelist()
	err := json.NewEncoder(w).Encode(&whitelistResponse)
	if err != nil {
		log.Println("Error encoding ping response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func ping(w http.ResponseWriter, r *http.Request, session *minienv.Session) {
	if r.Method != "POST" {
		http.Error(w, "Invalid ping request", 400)
	}
	if r.Body == nil {
		log.Println("Invalid ping request; Body is nil.")
		http.Error(w, "Invalid ping request", 400)
		return
	}
	var pingRequest minienv.PingRequest
	err := json.NewDecoder(r.Body).Decode(&pingRequest)
	if err != nil {
		log.Println("Error decoding ping request: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	pingResponse, err := apiServer.Ping(&pingRequest, session)
	if err != nil {
		log.Println("Error executing ping: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	err = json.NewEncoder(w).Encode(&pingResponse)
	if err != nil {
		log.Println("Error encoding ping response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func info(w http.ResponseWriter, r *http.Request, session *minienv.Session) {
	if r.Body == nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	var envInfoRequest minienv.EnvInfoRequest
	err := json.NewDecoder(r.Body).Decode(&envInfoRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	envInfoResponse, err := apiServer.Info(&envInfoRequest, session)
	if err != nil {
		http.Error(w, err.Error(), 401)
		return
	}
	err = json.NewEncoder(w).Encode(envInfoResponse)
	if err != nil {
		log.Print("Error encoding response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func up(w http.ResponseWriter, r *http.Request, session *minienv.Session) {
	if r.Body == nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	var envUpRequest minienv.EnvUpRequest
	err := json.NewDecoder(r.Body).Decode(&envUpRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	envUpResponse, err := apiServer.Up(&envUpRequest, session)
	if err != nil {
		http.Error(w, err.Error(), 401)
		return
	}
	err = json.NewEncoder(w).Encode(envUpResponse)
	if err != nil {
		log.Print("Error encoding response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	if _, err := strconv.Atoi(os.Args[1]); err != nil {
		log.Fatalf("Invalid port: %s (%s)\n", os.Args[1], err)
	}
	log.Println("Starting API Server...")
	apiServer = &minienv.ApiServer{}
	apiServer.Init()
	http.HandleFunc("/", root)
	http.HandleFunc("/me", apiServer.AddCorsAndCacheHeadersThenServe(me))
	http.HandleFunc("/claim", apiServer.AddCorsAndCacheHeadersThenServe(getSessionThenServe(claim)))
	http.HandleFunc("/ping", apiServer.AddCorsAndCacheHeadersThenServe(getSessionThenServe(ping)))
	http.HandleFunc("/info", apiServer.AddCorsAndCacheHeadersThenServe(getSessionThenServe(info)))
	http.HandleFunc("/up", apiServer.AddCorsAndCacheHeadersThenServe(getSessionThenServe(up)))
	http.HandleFunc("/whitelist", apiServer.AddCorsAndCacheHeadersThenServe(getSessionThenServe(whitelist)))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}