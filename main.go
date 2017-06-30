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

	"github.com/google/uuid"
)

var maxEnvironments = 2
var environments map[string]*Environment = make(map[string]*Environment)
var examplePvTemplate string
var examplePvcTemplate string
var exampleDeploymentTemplate string
var exampleServiceTemplate string
var kubeServiceToken string
var kubeServiceBaseUrl string
var kubeNamespace string
var storageDriver string
var allowOrigin string

type Environment struct {
	ClaimId string
	ClaimToken string
	UpRequest *UpRequest
	UpResponse *UpResponse
}

type ClaimRequest struct {
	Authorization string `json:"authorization"` // TODO:future support authentication
}

type ClaimResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	ClaimToken string `json:"claimToken"`
	ClaimId string `json:"claimId"`
	Message string `json:"message"`
}

type PingRequest struct {
	ClaimToken string `json:"claimToken"`
	GetUpDetails bool `json:"getUpDetails"`
}

type PingResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	Up bool `json:"up"`
	UpDetails *UpResponse `json:"upDetails"`
}

type UpRequest struct {
	ClaimToken string `json:"claimToken"`
	Repo string `json:"repo"`
}

type UpResponse struct {
	Repo string `json:"repo"`
	LogUrl string `json:"logUrl"`
	EditorUrl string `json:"editorUrl"`
	Tabs *[]*Tab `json:"tabs"`
	DeployToBluemix bool `json:"deployToBluemix"`
}

func claim(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid claim request", 400)
	}
	if r.Body == nil {
		log.Println("Invalid claim request; Body is nil.")
		http.Error(w, "Invalid claim request", 400)
		return
	}
	// decode request
	var claimRequest ClaimRequest
	err := json.NewDecoder(r.Body).Decode(&claimRequest)
	if err != nil {
		log.Println("Error decoding claim request: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	// create response
	var claimResponse = ClaimResponse{}
	if len(environments) >= maxEnvironments {
		// max environment exceeded - do not grant claim
		claimResponse.ClaimGranted = false
		claimResponse.Message = "No more claims available"
	} else {
		// ok, grant claim and create new environment
		claimToken, _ := uuid.NewRandom()
		claimResponse.ClaimGranted = true
		claimResponse.ClaimToken = claimToken.String()
		claimResponse.ClaimId = strconv.Itoa(len(environments) + 1)
		environment := &Environment{ClaimId: claimResponse.ClaimId, ClaimToken: claimResponse.ClaimToken}
		environments[claimResponse.ClaimToken] = environment
	}
	err = json.NewEncoder(w).Encode(&claimResponse)
	if err != nil {
		log.Println("Error encoding claim response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func ping(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid ping request", 400)
	}
	if r.Body == nil {
		log.Println("Invalid ping request; Body is nil.")
		http.Error(w, "Invalid ping request", 400)
		return
	}
	// decode request
	var pingRequest PingRequest
	err := json.NewDecoder(r.Body).Decode(&pingRequest)
	if err != nil {
		log.Println("Error decoding ping request: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	var pingResponse= PingResponse{}
	environment, ok := environments[pingRequest.ClaimToken]
	if ! ok {
		pingResponse.ClaimGranted = false
		pingResponse.Up = false
	} else {
		pingResponse.ClaimGranted = true
		pingResponse.Up = environment.UpRequest != nil && environment.UpResponse != nil
		if pingResponse.Up && pingRequest.GetUpDetails {
			// make sure to check if it is really running
			exists, err := isExampleDeployed(environment.ClaimId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Println("Error querying Kubernetes: ", err)
				http.Error(w, err.Error(), 400)
				return
			}
			pingResponse.Up = exists
			if exists {
				pingResponse.UpDetails = environment.UpResponse
			} else {
				environment.UpRequest = nil
				environment.UpResponse = nil
			}
		}
	}
	err = json.NewEncoder(w).Encode(&pingResponse)
	if err != nil {
		log.Println("Error encoding ping response: ", err)
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
	environment, ok := environments[upRequest.ClaimToken]
	if ! ok {
		log.Println("Up request failed; claim no longer valid.")
		http.Error(w, err.Error(), 401)
		return
	} else {
		// create response
		var upResponse *UpResponse
		log.Printf("Checking if deployment exists for claim '%s'...\n", environment.ClaimId)
		exists, err := isExampleDeployed(environment.ClaimId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Printf("Error checking if deployment exists for claim '%s': %s\n", environment.ClaimId, err)
			http.Error(w, err.Error(), 400)
			return
		} else if exists {
			log.Printf("Example deployed for claim '%s'.\n", environment.ClaimId)
			if environment.UpResponse != nil && environment.UpRequest != nil && strings.EqualFold(upRequest.Repo, environment.UpRequest.Repo) {
				log.Println("Returning existing environment details...")
				upResponse = environment.UpResponse
			}
		}
		if upResponse == nil {
			log.Println("Creating new deployment...")
			details, err := deployExample(environment.ClaimId, upRequest.Repo, storageDriver, examplePvTemplate, examplePvcTemplate, exampleDeploymentTemplate, exampleServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Print("Error creating deployment: ", err)
				http.Error(w, err.Error(), 400)
				return
			} else {
				upResponse = &UpResponse{}
				upResponse.Repo = upRequest.Repo
				// TODO: this should be a readme instead - that way it can support anything
				upResponse.DeployToBluemix = isManifestInRepo(upRequest.Repo)
				upResponse.LogUrl = details.LogUrl
				upResponse.EditorUrl = details.EditorUrl
				upResponse.Tabs = details.Tabs
				environment.UpRequest = &upRequest
				environment.UpResponse = upResponse
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
	kubeNamespace = os.Getenv("MINIENV_NAMESPACE")
	if kubeNamespace == "" {
		kubeNamespace = "default"
	}
	storageDriver = os.Getenv("MINIENV_STORAGE_DRIVER")
	if storageDriver == "" {
		storageDriver = "aufs"
	}
	allowOrigin = os.Getenv("MINIENV_ALLOW_ORIGIN")
	http.HandleFunc("/api/claim", addCorsAndCacheHeadersThenServe(claim))
	http.HandleFunc("/api/ping", addCorsAndCacheHeadersThenServe(ping))
	http.HandleFunc("/api/up", addCorsAndCacheHeadersThenServe(up))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
