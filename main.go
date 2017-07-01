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
	"time"

	"github.com/google/uuid"
)

var STATUS_IDLE = 0
var STATUS_PROVISIONING = 1
var STATUS_CLAIMED = 2
var STATUS_RUNNING = 3

var CHECK_ENV_TIMER_SECONDS = 15
var DELETE_ENV_NO_ACIVITY_SECONDS int64 = 60

var environments []*Environment
var examplePvTemplate string
var examplePvcTemplate string
var exampleDeploymentTemplate string
var exampleServiceTemplate string
var provisionerJobTemplate string
var kubeServiceToken string
var kubeServiceBaseUrl string
var kubeNamespace string
var storageDriver string
var allowOrigin string

type Environment struct {
	Id string
	Status int
	ClaimToken string
	UpRequest *UpRequest
	UpResponse *UpResponse
	LastActivity int64
}

type ClaimRequest struct {
	Authorization string `json:"authorization"` // TODO:future support authentication
}

type ClaimResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	ClaimToken string `json:"claimToken"`
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
	var environment *Environment
	for _, element := range environments {
		if element.Status == STATUS_IDLE {
			environment = element
			break
		}
	}
	if environment == nil {
		log.Println("No more claims available.")
		claimResponse.ClaimGranted = false
		claimResponse.Message = "No more claims available"
	} else {
		log.Printf("Claimed environment %s.\n", environment.Id)
		// ok, grant claim and create new environment
		claimToken, _ := uuid.NewRandom()
		claimResponse.ClaimGranted = true
		claimResponse.ClaimToken = claimToken.String()
		// update environment
		environment.ClaimToken = claimResponse.ClaimToken
		environment.Status = STATUS_CLAIMED
		environment.LastActivity = time.Now().Unix()
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
	var pingResponse = PingResponse{}
	var environment *Environment
	for _, element := range environments {
		if element.ClaimToken == pingRequest.ClaimToken {
			environment = element
			break
		}
	}
	if environment == nil {
		pingResponse.ClaimGranted = false
		pingResponse.Up = false
	} else {
		environment.LastActivity = time.Now().Unix()
		pingResponse.ClaimGranted = true
		pingResponse.Up = environment.UpRequest != nil && environment.UpResponse != nil
		if pingResponse.Up && pingRequest.GetUpDetails {
			// make sure to check if it is really running
			exists, err := isExampleDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
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
	var environment *Environment
	for _, element := range environments {
		if element.ClaimToken == upRequest.ClaimToken {
			environment = element
			break
		}
	}
	if environment == nil {
		log.Println("Up request failed; claim no longer valid.")
		http.Error(w, "Invalid claim token", 401)
		return
	} else {
		// create response
		var upResponse *UpResponse
		log.Printf("Checking if deployment exists for env %s...\n", environment.Id)
		exists, err := isExampleDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Printf("Error checking if deployment exists for env %s: %s\n", environment.Id, err)
			http.Error(w, err.Error(), 400)
			return
		} else if exists {
			log.Printf("Example deployed for claim %s.\n", environment.Id)
			if environment.Status == STATUS_RUNNING && strings.EqualFold(upRequest.Repo, environment.UpRequest.Repo) {
				log.Println("Returning existing environment details...")
				upResponse = environment.UpResponse
			}
		}
		if upResponse == nil {
			log.Println("Creating new deployment...")
			details, err := deployExample(environment.Id, upRequest.Repo, storageDriver, examplePvTemplate, examplePvcTemplate, exampleDeploymentTemplate, exampleServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
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
				environment.Status = STATUS_RUNNING
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

func initEnvironments(envCount int) {
	log.Printf("Provisioning %d environments...\n", envCount)
	for i := 0; i < envCount; i++ {
		environment := &Environment{Id: strconv.Itoa(i + 1)}
		environments = append(environments, environment)
		// check if environment running
		deployed, err := isExampleDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err == nil && deployed {
			log.Printf("Loading running environment %d...\n", environment.Id)
			environment.Status = STATUS_RUNNING
			// TODO: environment.ClaimToken =
			environment.LastActivity = time.Now().Unix()
			// TODO: environment.UpRequest = ???
			// TODO: environment.UpResponse = ???
		} else {
			log.Printf("Provisioning environment %d...\n", environment.Id)
			environment.Status = STATUS_PROVISIONING
			deployProvisioner(environment.Id, storageDriver, examplePvTemplate, examplePvcTemplate, provisionerJobTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		}
	}
	startEnvironmentCheckTimer()
}

func startEnvironmentCheckTimer() {
	timer := time.NewTimer(time.Second * time.Duration(CHECK_ENV_TIMER_SECONDS))
	go func() {
		<-timer.C
		checkEnvironments()
		startEnvironmentCheckTimer()
	}()
}

func checkEnvironments() {
	for i := 0; i < len(environments); i++ {
		environment := environments[i]
		log.Printf("Checking environment %s; current status=%d\n", environment.Id, environment.Status)
		if environment.Status == STATUS_PROVISIONING {
			running, err := isProvisionerRunning(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Println("Error checking provisioner status.", err)
			} else if ! running {
				log.Printf("Environment %s provisioning complete.\n", environment.Id)
				environment.Status = STATUS_IDLE
				deleteProvisioner(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				log.Printf("Environment %s still provisioning...\n", environment.Id)
			}
		} else if environment.Status == STATUS_RUNNING {
			if time.Now().Unix() - environment.LastActivity > DELETE_ENV_NO_ACIVITY_SECONDS {
				log.Printf("Environment %s no longer active.\n", environment.Id)
				environment.Status = STATUS_IDLE
				environment.ClaimToken = ""
				environment.UpRequest = nil
				environment.UpResponse = nil
				environment.LastActivity = 0
				deleteExample(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				log.Printf("Checking if environment %s is still deployed...\n", environment.Id)
				deployed, err := isExampleDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				if err == nil && ! deployed {
					log.Printf("Environment %s no longer deployed.\n", environment.Id)
					environment.Status = STATUS_IDLE
					environment.ClaimToken = ""
					environment.UpRequest = nil
					environment.UpResponse = nil
					environment.LastActivity = 0
				}
			}
		}
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
	provisionerJobTemplate = loadFile("./provisioner-job.yml")
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
	envCount := 1
	if i, err := strconv.Atoi(os.Getenv("MINIENV_PROVISION_COUNT")); err == nil {
		envCount = i
	}
	initEnvironments(envCount)
	http.HandleFunc("/api/claim", addCorsAndCacheHeadersThenServe(claim))
	http.HandleFunc("/api/ping", addCorsAndCacheHeadersThenServe(ping))
	http.HandleFunc("/api/up", addCorsAndCacheHeadersThenServe(up))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
