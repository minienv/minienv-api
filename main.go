package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var deployments map[string]*Deployment = make(map[string]*Deployment)
var examplePvTemplate string
var examplePvcTemplate string
var exampleDeploymentTemplate string
var exampleServiceTemplate string
var kubeServiceToken string
var kubeServiceBaseUrl string
var kubeNamespace string
var storageDriver string
var allowOrigin string

type Deployment struct {
	UserId string
	UpRequest *UpRequest
	UpResponse *UpResponse
}

type PingRequest struct {
	UserId string `json:"userId"`
	GetUpDetails bool `json:"getUpDetails"`
}

type PingResponse struct {
	Up bool `json:"up"`
	UpDetails *UpResponse `json:"upDetails"`
}

type UpRequest struct {
	UserId string `json:"userId"`
	Repo string `json:"repo"`
}

type UpResponse struct {
	Repo string `json:"repo"`
	LogUrl string `json:"logUrl"`
	EditorUrl string `json:"editorUrl"`
	Tabs *[]*Tab `json:"tabs"`
	DeployToBluemix bool `json:"deployToBluemix"`
}

func ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid request", 400)
	}
	if r.Body == nil {
		log.Println("Invalid request; Body is nil.")
		http.Error(w, "Invalid request", 400)
		return
	}
	// decode request
	var pingRequest PingRequest
	err := json.NewDecoder(r.Body).Decode(&pingRequest)
	if err != nil {
		log.Println("Error decoding request: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	// create response
	var pingResponse = PingResponse{}
	deployment, ok := deployments[pingRequest.UserId]
	if ok {
		pingResponse.Up = true
		if pingRequest.GetUpDetails {
			// make sure to check if it is really running
			exists, err := isExampleDeployed(pingRequest.UserId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Println("Error querying Kubernetes: ", err)
				http.Error(w, err.Error(), 400)
				return
			}
			pingResponse.Up = exists
			if exists {
				pingResponse.UpDetails = deployment.UpResponse
			} else {
				deployments[pingRequest.UserId] = nil
			}
		}
	}
	err = json.NewEncoder(w).Encode(&pingResponse)
	if err != nil {
		log.Println("Error encoding response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func up(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	// decode request
	var upRequest UpRequest
	err := json.NewDecoder(r.Body).Decode(&upRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	// create response
	var upResponse *UpResponse
	// call kubernetes
	log.Printf("Checking if deployment exists for user '%s'...\n", upRequest.UserId)
	exists, err := isExampleDeployed(upRequest.UserId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Printf("Error checking if deployment exists for user '%s': %s\n", upRequest.UserId, err)
		http.Error(w, err.Error(), 400)
		return
	} else if exists {
		log.Printf("Example deployed for user '%s'.\n", upRequest.UserId)
		deployment, ok := deployments[upRequest.UserId]
		if ok &&  strings.EqualFold(upRequest.Repo, deployment.UpRequest.Repo) {
			log.Println("Returning existing deployment details...")
			upResponse = deployment.UpResponse
		}
	}
	if upResponse == nil  {
		log.Println("Creating new deployment...")
		details, err := deployExample(upRequest.UserId, upRequest.Repo, storageDriver, examplePvTemplate, examplePvcTemplate, exampleDeploymentTemplate, exampleServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Print("Error creating deployment: ", err)
			http.Error(w, err.Error(), 400)
			return
		} else {
			upResponse = &UpResponse{}
			upResponse.Repo = upRequest.Repo
			upResponse.DeployToBluemix = isManifestInRepo(upRequest.Repo)
			upResponse.LogUrl = details.LogUrl
			upResponse.EditorUrl = details.EditorUrl
			upResponse.Tabs = details.Tabs
			deployments[upRequest.UserId] = &Deployment{upRequest.UserId, &upRequest, upResponse}
		}
	}
	// return response
	err = json.NewEncoder(w).Encode(upResponse)
	if err != nil {
		log.Print("Error encoding response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func isManifestInRepo(gitRepo string) (bool) {
	return isFileInRepo(gitRepo, "manifest.yml") || isFileInRepo(gitRepo, "manifest.yaml")
}

func isFileInRepo(gitRepo string, file string) (bool) {
	url := fmt.Sprintf("%s/raw/master/%s", gitRepo, file)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	res, err := client.Do(req)
	if err != nil || res.StatusCode == 404 {
		return false
	} else {
		return true
	}
}

func loadFile(fp string) string {
	b, err := ioutil.ReadFile(fp) // just pass the file name
	if err != nil {
		log.Fatalf("Cannot read file")
	}
	return string(b)
}

func addCorsAndCacheHeadersThenServe(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Cache-Control", "no-store, must-revalidate")
		w.Header().Add("Expires", "0")
		if r.Method == "OPTIONS" {
			return
		}
		handler(w, r)
	}
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	if _, err := strconv.Atoi(os.Args[1]); err != nil {
		log.Fatalf("Invalid port: %s (%s)\n", os.Args[1], err)
	}
	examplePvTemplate = loadFile("./example-pv.yml")
	examplePvcTemplate = loadFile("./example-pvc.yml")
	exampleDeploymentTemplate = loadFile("./example-deployment.yml")
	exampleServiceTemplate = loadFile("./example-service.yml")
	kubeServiceProtocol := os.Getenv("KUBERNETES_SERVICE_PROTOCOL")
	kubeServiceHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	kubeServicePort := os.Getenv("KUBERNETES_SERVICE_PORT")
	kubeServiceTokenPathEnv := os.Getenv("KUBERNETES_TOKEN_PATH")
	if len(kubeServiceTokenPathEnv) > 0 {
		kubeServiceToken = loadFile(kubeServiceTokenPathEnv)
	} else {
		kubeServiceToken = ""
	}
	if len(kubeServiceProtocol) > 0 {
		kubeServiceBaseUrl = kubeServiceProtocol
	} else {
		kubeServiceBaseUrl = "https://"
	}
	kubeServiceBaseUrl += kubeServiceHost
	kubeServiceBaseUrl += ":"
	kubeServiceBaseUrl += kubeServicePort
	kubeNamespace = os.Getenv("EXUP_NAMESPACE")
	if kubeNamespace == "" {
		kubeNamespace = "default"
	}
	storageDriver = os.Getenv("EXUP_STORAGE_DRIVER")
	if storageDriver == "" {
		storageDriver = "aufs"
	}
	allowOrigin = os.Getenv("EXUP_ALLOW_ORIGIN")
	http.HandleFunc("/api/ping", addCorsAndCacheHeadersThenServe(ping))
	http.HandleFunc("/api/up", addCorsAndCacheHeadersThenServe(up))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
