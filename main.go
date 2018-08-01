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
	"bytes"

)

const StatusIdle = 0
const StatusProvisioning = 1
const StatusClaimed = 2
const StatusRunning = 3

const CheckEnvTimerSeconds = 15
const ExpireClaimNoActivitySeconds int64 = 30
const DefaultEnvExpirationSeconds int64 = 60
const DefaultBranch = "master"

var minienvVersion = "latest"
var githubAuthEnabled = false
var githubAuthUsers map[string]*GitHubAuthUser
var githubClientId string
var githubClientSecret string
var environments []*Environment
var envPvHostPath bool
var envPvTemplate string
var envPvcTemplate string
var envPvcStorageClass string
var envDeploymentTemplate string
var envServiceTemplate string
var provisionerJobTemplate string
var provisionVolumeSize string
var provisionImages string
var kubeServiceToken string
var kubeServiceBaseUrl string
var kubeNamespace string
var nodeNameOverride string
var storageDriver string
var allowOrigin string
var whitelistRepos []*WhitelistRepo

type GitHubAuthUser struct {
	Email string `json:"email"`
}

type MeResponse struct {
	User *GitHubAuthUser `json:"user"`
}

type GitHubAuthTokenRequest struct {
	ClientId string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code string `json:"code"`
}

type GitHubAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type WhitelistRepo struct {
	Name string `json:"name"`
	Url string `json:"url"`
	Branch string `json:"branch"`
}

type Environment struct {
	Id string
	Status int
	ClaimToken string
	LastActivity int64
	Repo string
	Branch string
	Details *EnvUpResponse
	ExpirationSeconds int64
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
	Repos []*WhitelistRepo `json:"repos"`
}

type PingRequest struct {
	ClaimToken string `json:"claimToken"`
	GetEnvDetails bool `json:"getEnvDetails"`
}

type PingResponse struct {
	ClaimGranted bool `json:"claimGranted"`
	Up bool `json:"up"`
	Repo string `json:"repo"`
	Branch string `json:"branch"`
	EnvDetails *EnvUpResponse `json:"envDetails"`
}

type EnvInfoRequest struct {
	Repo string `json:"repo"`
	Branch string `json:"branch"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type EnvInfoResponse struct {
	Env *EnvInfoResponseEnv `json:"env"`
}

type EnvInfoResponseEnv struct {
	Vars *[]EnvInfoResponseEnvVar `json:"vars"`
}

type EnvInfoResponseEnvVar struct {
	Name string `json:"name"`
	DefaultValue string `json:"defaultValue"`
}

type EnvUpRequest struct {
	ClaimToken string `json:"claimToken"`
	Repo string `json:"repo"`
	Branch string `json:"branch"`
	Username string `json:"username"`
	Password string `json:"password"`
	ExpirationSeconds int64 `json:"expirationSeconds"`
	EnvVars map[string]string `json:"envVars"`
}

type EnvUpResponse struct {
	LogUrl string `json:"logUrl"`
	EditorUrl string `json:"editorUrl"`
	Tabs *[]*Tab `json:"tabs"`
}

func root(w http.ResponseWriter, r *http.Request) {
}

func me(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid me request", 400)
	}
	accessToken := r.Header.Get("X-Access-Token")
	if accessToken == "" {
		http.Error(w, "Not authenticated", 401)
	} else {
		githubUser := githubAuthUsers[accessToken]
		if githubUser != nil {
			meResponse := MeResponse{
				User: githubUser,
			}
			err := json.NewEncoder(w).Encode(&meResponse)
			if err != nil {
				log.Println("Error encoding me response: ", err)
				http.Error(w, err.Error(), 400)
				return
			}
		} else {
			http.Error(w, "Not authenticated", 401)
		}
	}
}

func authCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid auth callback request", 400)
	}
	codeVals, ok := r.URL.Query()["code"]
	if !ok || len(codeVals[0]) < 1 {
		http.Error(w, "code missing", 400)
		return
	}
	url := "https://github.com/login/oauth/access_token"
	authTokenRequest := GitHubAuthTokenRequest{
		ClientId: githubClientId,
		ClientSecret: githubClientSecret,
		Code: codeVals[0],
	}
	b, err := json.Marshal(authTokenRequest)
	if err != nil {
		log.Println("Error serializing auth token request: ", err)
		http.Error(w, "error serializing auth token request", 400)
		return
	}
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting access token: ", err)
		http.Error(w, "error getting access token", 400)
		return
	} else {
		var authTokenResponse GitHubAuthTokenResponse
		err := json.NewDecoder(resp.Body).Decode(&authTokenResponse)
		if err != nil {
			log.Println("Error getting access token: ", err)
			http.Error(w, "error getting access token", 400)
			return
		} else {
			log.Println("Access token: ", authTokenResponse.AccessToken)
			githubAuthUsers[authTokenResponse.AccessToken] = &GitHubAuthUser{
				Email: authTokenResponse.AccessToken,
			}
			stateVals, ok := r.URL.Query()["state"]
			if ok && len(stateVals[0]) >= 1 {
				state := stateVals[0]
				log.Println("State: ", state)
				redirectUrl := strings.Replace(state, "$accessToken", authTokenResponse.AccessToken, -1)
				http.Redirect(w, r, redirectUrl, 301)
			} else {
				http.Redirect(w, r, "/", 301)
			}

		}
	}
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
		if element.Status == StatusIdle {
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
		environment.Status = StatusClaimed
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
		pingResponse.Up = environment.Status == StatusRunning
		pingResponse.Repo = environment.Repo
		pingResponse.Branch = environment.Branch
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
				environment.Status = StatusClaimed
				environment.Repo = ""
				environment.Branch = ""
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

func info(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	// decode request
	var envInfoRequest EnvInfoRequest
	err := json.NewDecoder(r.Body).Decode(&envInfoRequest)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	} else if envInfoRequest.Branch == "" {
		envInfoRequest.Branch = DefaultBranch
	}

	if whitelistRepos != nil {
		repoWhitelisted := false
		for _, element := range whitelistRepos {
			if envInfoRequest.Repo == element.Url && envInfoRequest.Branch == element.Branch {
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
	var envInfoResponse = &EnvInfoResponse{}
	minienvConfig, err := downloadMinienvConfig(envInfoRequest.Repo, envInfoRequest.Branch, envInfoRequest.Username, envInfoRequest.Password)
	if err != nil {
		log.Print("Error getting minienv config: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	if minienvConfig.Env != nil {
		envVars := []EnvInfoResponseEnvVar{}
		for _, configEnvVar := range *minienvConfig.Env.Vars {
			envVar := EnvInfoResponseEnvVar{}
			envVar.Name = configEnvVar.Name
			envVar.DefaultValue = configEnvVar.DefaultValue
			envVars = append(envVars, envVar)
		}
		envInfoResponse.Env = &EnvInfoResponseEnv{}
		envInfoResponse.Env.Vars = &envVars
	}

	// return response
	err = json.NewEncoder(w).Encode(envInfoResponse)
	if err != nil {
		log.Print("Error encoding response: ", err)
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
	} else if envUpRequest.Branch == "" {
		envUpRequest.Branch = DefaultBranch
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
				if envUpRequest.Repo == element.Url && envUpRequest.Branch == element.Branch {
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
			if environment.Status == StatusRunning && strings.EqualFold(envUpRequest.Repo, environment.Repo) && strings.EqualFold(envUpRequest.Branch, environment.Branch) {
				log.Println("Returning existing environment details...")
				envUpResponse = environment.Details
			}
		}
		if envUpResponse == nil {
			log.Println("Creating new deployment...")
			// change status to claimed, so the scheduler doesn't think it has stopped when the old repo is shutdown
			environment.Status = StatusClaimed
			details, err := deployEnv(minienvVersion, environment.Id, environment.ClaimToken, nodeNameOverride, envUpRequest.Repo, envUpRequest.Branch, envUpRequest.Username, envUpRequest.Password, envUpRequest.EnvVars, storageDriver, envPvTemplate, envPvcTemplate, envDeploymentTemplate, envServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Print("Error creating deployment: ", err)
				http.Error(w, err.Error(), 400)
				return
			} else {
				envUpResponse = getEnvUpResponse(details)
				environment.Status = StatusRunning
				environment.Repo = envUpRequest.Repo
				environment.Branch = envUpRequest.Branch
				environment.Details = envUpResponse
				if envUpRequest.ExpirationSeconds >= 0 {
					environment.ExpirationSeconds = envUpRequest.ExpirationSeconds
				} else {
					environment.ExpirationSeconds = DefaultEnvExpirationSeconds
				}
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

func getEnvUpResponse(details *DeploymentDetails) (*EnvUpResponse) {
	envUpResponse := &EnvUpResponse{}
	envUpResponse.LogUrl = details.LogUrl
	envUpResponse.EditorUrl = details.EditorUrl
	envUpResponse.Tabs = details.Tabs
	return envUpResponse
}

func isFileInRepo(gitRepo string, gitBranch string, file string) (bool) {
	url := fmt.Sprintf("%s/raw/%s/%s", gitRepo, gitBranch, file)
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

func authorizeThenServe(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accessToken := r.Header.Get("X-Access-Token")
		if accessToken == "" {
			http.Error(w, "Not authenticated", 401)
			return
		} else if githubAuthUsers[accessToken] == nil {
			client := getHttpClient()
			url := "https://api.github.com/user?access_token=" + accessToken
			req, err := http.NewRequest("GET", url, nil)
			req.Header.Add("Accept", "application/json")
			_, err = client.Do(req)
			if err != nil {
				log.Println("Invalid access token: ", err)
				http.Error(w, "invalid access token", 400)
				return
			}
			githubAuthUsers[accessToken] = &GitHubAuthUser{
				Email: accessToken,
			}
		}
		handler(w, r)
	}
}

func addCorsAndCacheHeadersThenServe(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Access-Control-Allow-Headers", "X-Access-Token")
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
				envUpResponse := getEnvUpResponse(details)
				environment.Status = StatusRunning
				environment.ClaimToken = getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken
				environment.LastActivity = time.Now().Unix()
				environment.Repo = getDeploymentResp.Spec.Template.Metadata.Annotations.Repo
				environment.Branch = getDeploymentResp.Spec.Template.Metadata.Annotations.Branch
				environment.Details = envUpResponse
			} else {
				log.Printf("Insufficient deployment metadata for environment %s.\n", environment.Id)
				deleteEnv(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			}
		}
		if ! running {
			log.Printf("Provisioning environment %s...\n", environment.Id)
			environment.Status = StatusProvisioning
			deployProvisioner(minienvVersion, environment.Id, nodeNameOverride, storageDriver, envPvTemplate, envPvcTemplate, provisionerJobTemplate, provisionVolumeSize, provisionImages, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		}
	}
	// scale down, if necessary
	i := envCount
	for true {
		envId := strconv.Itoa(i + 1)
		pvcName := getPersistentVolumeClaimName(envId)
		response, _ := getPersistentVolumeClaim(pvcName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if response != nil {
			log.Printf("De-provisioning environment %s...\n", envId)
			deleteEnv(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			deleteProvisioner(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			deletePersistentVolumeClaim(pvcName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if envPvHostPath {
				pvName := getPersistentVolumeName(envId)
				deletePersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
			}
			i++
		} else {
			break
		}
	}
	checkEnvironments()
}

func startEnvironmentCheckTimer() {
	timer := time.NewTimer(time.Second * time.Duration(CheckEnvTimerSeconds))
	go func() {
		<-timer.C
		checkEnvironments()
	}()
}

func checkEnvironments() {
	for i := 0; i < len(environments); i++ {
		environment := environments[i]
		log.Printf("Checking environment %s; current status=%d\n", environment.Id, environment.Status)
		if environment.Status == StatusProvisioning {
			running, err := isProvisionerRunning(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil {
				log.Println("Error checking provisioner status.", err)
			} else if ! running {
				log.Printf("Environment %s provisioning complete.\n", environment.Id)
				environment.Status = StatusIdle
				deleteProvisioner(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				log.Printf("Environment %s still provisioning...\n", environment.Id)
			}
		} else if environment.Status == StatusRunning {
			if time.Now().Unix() - environment.LastActivity > DefaultEnvExpirationSeconds {
				log.Printf("Environment %s no longer active.\n", environment.Id)
				environment.Status = StatusIdle
				environment.ClaimToken = ""
				environment.LastActivity = 0
				environment.Repo = ""
				environment.Branch = ""
				environment.Details = nil
				deleteEnv(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				// re-provision
				log.Printf("Re-provisioning environment %s...\n", environment.Id)
				environment.Status = StatusProvisioning
				deployProvisioner(minienvVersion, environment.Id, nodeNameOverride, storageDriver, envPvTemplate, envPvcTemplate, provisionerJobTemplate, provisionVolumeSize, provisionImages, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				log.Printf("Checking if environment %s is still deployed...\n", environment.Id)
				deployed, err := isEnvDeployed(environment.Id, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				if err == nil && ! deployed {
					log.Printf("Environment %s no longer deployed.\n", environment.Id)
					environment.Status = StatusIdle
					environment.ClaimToken = ""
					environment.LastActivity = 0
					environment.Repo = ""
					environment.Branch = ""
					environment.Details = nil
				}
			}
		}  else if environment.Status == StatusClaimed {
			if time.Now().Unix() - environment.LastActivity > ExpireClaimNoActivitySeconds {
				log.Printf("Environment %s claim expired.\n", environment.Id)
				environment.Status = StatusIdle
				environment.ClaimToken = ""
				environment.LastActivity = 0
				environment.Repo = ""
				environment.Branch = ""
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
	minienvVersion = os.Getenv("MINIENV_VERSION")
	githubClientId = os.Getenv("MINIENV_GITHUB_CLIENT_ID")
	githubClientSecret = os.Getenv("MINIENV_GITHUB_CLIENT_SECRET")
	githubAuthEnabled = githubClientId != "" && githubClientSecret != ""
	githubAuthUsers = make(map[string]*GitHubAuthUser)
	envPvcStorageClass = os.Getenv("MINIENV_VOLUME_STORAGE_CLASS")
	if envPvcStorageClass == "" {
		envPvHostPath = true
		envPvTemplate = loadFile("./env-pv-host-path.yml")
		envPvcTemplate = loadFile("./env-pvc-host-path.yml")

	} else {
		envPvHostPath = false
		envPvcTemplate = loadFile("./env-pvc-storage-class.yml")
	}
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
	nodeNameOverride = os.Getenv("MINIENV_NODE_NAME_OVERRIDE")
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
		whitelistRepoStrs := strings.Split(whitelistReposStr, ",")
		if len(whitelistRepoStrs) <= 0 {
			whitelistRepos = nil
		} else {
			whitelistRepos = []*WhitelistRepo{}
			var name string
			var url string
			var branch string
			for _, element := range whitelistRepoStrs {
				elementStrs := strings.Split(element, "|")
				if len(elementStrs) >= 2 {
					name = elementStrs[0]
					url = elementStrs[1]
					if len(elementStrs) == 3 {
						branch = elementStrs[2]
					} else {
						branch = DefaultBranch
					}
				} else {
					name = element
					url = element
					branch = DefaultBranch
				}
				whitelistRepo := &WhitelistRepo{Name: name, Url: url, Branch: branch}
				whitelistRepos = append(whitelistRepos, whitelistRepo)
			}
		}
	}
	initEnvironments(envCount)
	http.HandleFunc("/", root)
	http.HandleFunc("/auth/callback", authCallback)
	http.HandleFunc("/api/me", addCorsAndCacheHeadersThenServe(me))
	http.HandleFunc("/api/claim", addCorsAndCacheHeadersThenServe(authorizeThenServe(claim)))
	http.HandleFunc("/api/ping", addCorsAndCacheHeadersThenServe(authorizeThenServe(ping)))
	http.HandleFunc("/api/info", addCorsAndCacheHeadersThenServe(authorizeThenServe(info)))
	http.HandleFunc("/api/up", addCorsAndCacheHeadersThenServe(authorizeThenServe(up)))
	http.HandleFunc("/api/whitelist", addCorsAndCacheHeadersThenServe(authorizeThenServe(whitelist)))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
