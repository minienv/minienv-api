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
var EXPIRE_CLAIM_NO_ACIVITY_SECONDS int64 = 30

var environments []*Environment
var envPvTemplate string
var envPvcTemplate string
var envDeploymentTemplate string
var envServiceTemplate string
var provisionerJobTemplate string
var provisionVolumeSize string
var provisionImages string
var kubeServiceToken string
var kubeServiceBaseUrl string
var kubeNamespace string
var storageDriver string
var allowOrigin string
var whitelistRepos []string

type Environment struct {
	Id string
	Status int
	ClaimToken string
	LastActivity int64
	Repo string
	Details *EnvUpResponse
}

type ClaimRequest struct {
	Authorization string `json:"authorization"` // TODO:future support authentication
}

type ClaimResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	ClaimToken string `json:"claimToken"`
	Message string `json:"message"`
}

type WhitelistResponse struct {
	Repos []string `json:"repos"`
}

type PingRequest struct {
	ClaimToken string `json:"claimToken"`
	GetEnvDetails bool `json:"getEnvDetails"`
}

type PingResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	Up bool `json:"up"`
	Repo string `json:"repo"`
	EnvDetails *EnvUpResponse `json:"envDetails"`
}

type EnvUpRequest struct {
	ClaimToken string `json:"claimToken"`
	Repo string `json:"repo"`
}

type EnvUpResponse struct {
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
		log.Println("Claim failed; no environments available.")
		claimResponse.ClaimGranted = false
		claimResponse.Message = "No environments available"
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

func whitelist(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid whitelist request", 400)
	}
	var whitelistResponse = WhitelistResponse{}
	whitelistResponse.Repos = whitelistRepos
	err := json.NewEncoder(w).Encode(&whitelistResponse)
	if err != nil {
		log.Println("Error encoding ping response: ", err)
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
		pingResponse.Up = environment.Status == STATUS_RUNNING
		pingResponse.Repo = environment.Repo;
		if pingResponse.Up && pingRequest.GetEnvDetails {
			// make sure to check if it is really running
			exists, err := isEnvDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Println("Error querying Kubernetes: ", err)
				http.Error(w, err.Error(), 400)
				return
			}
			pingResponse.Up = exists
			if exists {
				pingResponse.EnvDetails = environment.Details
			} else {
				environment.Status = STATUS_CLAIMED
				environment.Repo = ""
				environment.Details = nil
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
	var envUpRequest EnvUpRequest
	err := json.NewDecoder(r.Body).Decode(&envUpRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var environment *Environment
	for _, element := range environments {
		if element.ClaimToken == envUpRequest.ClaimToken {
			environment = element
			break
		}
	}
	if environment == nil {
		log.Println("Up request failed; claim no longer valid.")
		http.Error(w, "Invalid claim token", 401)
		return
	} else {
		if whitelistRepos != nil {
			repoWhitelisted := false
			for _, element := range whitelistRepos {
				if envUpRequest.Repo == element {
					repoWhitelisted = true
					break
				}
			}
			if ! repoWhitelisted {
				log.Println("Up request failed; repo not whitelisted.")
				http.Error(w, "Invalid repo", 401)
				return
			}
		}
		// create response
		var envUpResponse *EnvUpResponse
		log.Printf("Checking if deployment exists for env %s...\n", environment.Id)
		exists, err := isEnvDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Printf("Error checking if deployment exists for env %s: %s\n", environment.Id, err)
			http.Error(w, err.Error(), 400)
			return
		} else if exists {
			log.Printf("Env deployed for claim %s.\n", environment.Id)
			if environment.Status == STATUS_RUNNING && strings.EqualFold(envUpRequest.Repo, environment.Repo) {
				log.Println("Returning existing environment details...")
				envUpResponse = environment.Details
			}
		}
		if envUpResponse == nil {
			log.Println("Creating new deployment...")
			// change status to claimed, so the scheduler doesn't think it has stopped when the old repo is shutdown
			environment.Status = STATUS_CLAIMED
			details, err := deployEnv(environment.Id, environment.ClaimToken, envUpRequest.Repo, storageDriver, envPvTemplate, envPvcTemplate, envDeploymentTemplate, envServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Print("Error creating deployment: ", err)
				http.Error(w, err.Error(), 400)
				return
			} else {
				envUpResponse = getEnvUpResponse(envUpRequest.Repo, details)
				environment.Status = STATUS_RUNNING
				environment.Repo = envUpRequest.Repo
				environment.Details = envUpResponse
			}
		}
		// return response
		err = json.NewEncoder(w).Encode(envUpResponse)
		if err != nil {
			log.Print("Error encoding response: ", err)
			http.Error(w, err.Error(), 400)
			return
		}
	}
}

func getEnvUpResponse(repo string, details *DeploymentDetails) (*EnvUpResponse) {
	envUpResponse := &EnvUpResponse{}
	// TODO: this should be a readme instead - that way it can support anything
	envUpResponse.DeployToBluemix = isManifestInRepo(repo)
	envUpResponse.LogUrl = details.LogUrl
	envUpResponse.EditorUrl = details.EditorUrl
	envUpResponse.Tabs = details.Tabs
	return envUpResponse
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
		getDeploymentResp, err := getEnvDeployment(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		running := false
		if err == nil && getDeploymentResp != nil {
			log.Printf("Loading running environment %s...\n", environment.Id)
			if getDeploymentResp.Spec != nil &&
				getDeploymentResp.Spec.Template != nil &&
				getDeploymentResp.Spec.Template.Metadata != nil &&
				getDeploymentResp.Spec.Template.Metadata.Annotations != nil &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.Repo != "" &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken != "" &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.EnvDetails != "" {
				log.Printf("Loading environment %s from deployment metadata.\n", environment.Id)
				running = true
				details  := deploymentDetailsFromString(getDeploymentResp.Spec.Template.Metadata.Annotations.EnvDetails)
				envUpResponse := getEnvUpResponse(getDeploymentResp.Spec.Template.Metadata.Annotations.Repo, details)
				environment.Status = STATUS_RUNNING
				environment.ClaimToken = getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken
				environment.LastActivity = time.Now().Unix()
				environment.Repo = getDeploymentResp.Spec.Template.Metadata.Annotations.Repo
				environment.Details = envUpResponse
			} else {
				log.Printf("Insufficient deployment metadata for environment %s.\n", environment.Id)
				deleteEnv(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			}
		}
		if ! running {
			log.Printf("Provisioning environment %s...\n", environment.Id)
			environment.Status = STATUS_PROVISIONING
			deployProvisioner(environment.Id, storageDriver, envPvTemplate, envPvcTemplate, provisionerJobTemplate, provisionVolumeSize, provisionImages, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		}
	}
	// scale down, if necessary
	i := envCount
	for true {
		envId := strconv.Itoa(i + 1)
		pvName := getPersistentVolumeName(envId)
		response, _ := getPersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
		if response != nil {
			log.Printf("De-provisioning environment %s...\n", envId)
			deleteEnv(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			deleteProvisioner(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			deletePersistentVolumeClaim(getPersistentVolumeClaimName(envId), kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			deletePersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
			i++
		} else {
			break
		}
	}
	checkEnvironments()
}

func startEnvironmentCheckTimer() {
	timer := time.NewTimer(time.Second * time.Duration(CHECK_ENV_TIMER_SECONDS))
	go func() {
		<-timer.C
		checkEnvironments()
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
				environment.LastActivity = 0
				environment.Repo = ""
				environment.Details = nil
				deleteEnv(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				// re-provision
				log.Printf("Re-provisioning environment %s...\n", environment.Id)
				environment.Status = STATUS_PROVISIONING
				deployProvisioner(environment.Id, storageDriver, envPvTemplate, envPvcTemplate, provisionerJobTemplate, provisionVolumeSize, provisionImages, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				log.Printf("Checking if environment %s is still deployed...\n", environment.Id)
				deployed, err := isEnvDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				if err == nil && ! deployed {
					log.Printf("Environment %s no longer deployed.\n", environment.Id)
					environment.Status = STATUS_IDLE
					environment.ClaimToken = ""
					environment.LastActivity = 0
					environment.Repo = ""
					environment.Details = nil
				}
			}
		}  else if environment.Status == STATUS_CLAIMED {
			if time.Now().Unix() - environment.LastActivity > EXPIRE_CLAIM_NO_ACIVITY_SECONDS {
				log.Printf("Environment %s claim expired.\n", environment.Id)
				environment.Status = STATUS_IDLE
				environment.ClaimToken = ""
				environment.LastActivity = 0
				environment.Repo = ""
				environment.Details = nil
			}
		}
	}
	startEnvironmentCheckTimer()
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	if _, err := strconv.Atoi(os.Args[1]); err != nil {
		log.Fatalf("Invalid port: %s (%s)\n", os.Args[1], err)
	}
	envPvTemplate = loadFile("./env-pv.yml")
	envPvcTemplate = loadFile("./env-pvc.yml")
	envDeploymentTemplate = loadFile("./env-deployment.yml")
	envServiceTemplate = loadFile("./env-service.yml")
	provisionerJobTemplate = loadFile("./provisioner-job.yml")
	provisionVolumeSize = os.Getenv("MINIENV_PROVISION_VOLUME_SIZE")
	provisionImages = os.Getenv("MINIENV_PROVISION_IMAGES")
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
	whitelistReposStr := os.Getenv("MINIENV_REPO_WHITELIST")
	if whitelistReposStr == "" {
		whitelistRepos = nil
	} else {
		whitelistRepos = strings.Split(whitelistReposStr, ",")
		if len(whitelistRepos) <= 0 {
			whitelistRepos = nil
		}
	}
	initEnvironments(envCount)
	http.HandleFunc("/api/claim", addCorsAndCacheHeadersThenServe(claim))
	http.HandleFunc("/api/ping", addCorsAndCacheHeadersThenServe(ping))
	http.HandleFunc("/api/up", addCorsAndCacheHeadersThenServe(up))
	http.HandleFunc("/api/whitelist", addCorsAndCacheHeadersThenServe(whitelist))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
