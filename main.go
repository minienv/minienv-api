package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
var authProvider AuthProvider
var sessionStore SessionStore
var userStore UserStore
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
var nodeHostProtocol string
var storageDriver string
var allowOrigin string
var whitelistRepos []*WhitelistRepo

type AuthHandlerFunc func(http.ResponseWriter, *http.Request, *User, *Session)

type AuthProvider interface {
	onAuthCallback(parameters map[string][]string) (*User, error)
	loginUser(accessToken string) (*User, error)
	userCanViewRepo(user *User, repo string) (bool, error)
}

type SessionStore interface {
	setSession(id string, session *Session) (error)
	getSession(id string) (*Session, error)
}

type UserStore interface {
	setUser(accessToken string, user *User) (error)
	getUser(accessToken string) (*User, error)
}

type User struct {
	AccessToken string `json:"accessToken"`
	Email string `json:"email"`
	Username string `json:"username"`
}

type Session struct {
	Id string  `json:"sessionId"`
	User *User `json:"user"`
	EnvId string `json:"envId"`
	EnvServiceName string `json:"envServiceName"`
}

type MeResponse struct {
	SessionId string `json:"sessionId"`
	Authenticated bool `json:"authenticated"`
	Username string `json:"username"`
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
	RepoWithCreds string
	Branch string
	Details *DeploymentDetails
	ExpirationSeconds int64
}

type ClaimRequest struct {
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
	Platform string `json:"platform"`
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
	Tabs []Tab `json:"tabs"`
}

func getOrCreateSession(r *http.Request) *Session {
	sessionId := r.Header.Get("Minienv-Session-Id")
	var session *Session = nil
	if sessionId != "" {
		session, _ = sessionStore.getSession(sessionId)
	}
	if session == nil {
		uuid, _ := uuid.NewRandom()
		sessionId = strings.Replace(uuid.String(), "-", "", -1)
		session = &Session{Id: sessionId, User: nil}
		sessionStore.setSession(sessionId, session)
	}
	return session
}

func root(w http.ResponseWriter, r *http.Request) {
}

func me(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Invalid me request", 400)
		return
	}
	meWithSession(w, r, getOrCreateSession(r))
}

func meWithSession(w http.ResponseWriter, r *http.Request, session *Session) {
	meResponse := MeResponse{
		SessionId: session.Id,
		Authenticated: session.User != nil,
	}
	err := json.NewEncoder(w).Encode(&meResponse)
	if err != nil {
		log.Println("Error encoding me response: ", err)
		http.Error(w, err.Error(), 400)
	}
}

func authCallback(w http.ResponseWriter, r *http.Request) {
	session := getOrCreateSession(r)
	if r.Method != "GET" {
		http.Error(w, "Invalid auth callback request", 400)
	}
	user, err := authProvider.onAuthCallback(r.URL.Query())
	if err != nil {
		http.Error(w, "Error authenticating user", 400)
		return
	}
	userStore.setUser(user.AccessToken, user)
	session.User = user
	sessionStore.setSession(session.Id, session)
	meWithSession(w, r, session)
}

func claim(w http.ResponseWriter, r *http.Request, user *User, session *Session) {
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
		claimTokenStr := strings.Replace(claimToken.String(), "-", "", -1)
		claimResponse.ClaimGranted = true
		claimResponse.ClaimToken = claimTokenStr
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

func whitelist(w http.ResponseWriter, r *http.Request, user *User, session *Session) {
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

func ping(w http.ResponseWriter, r *http.Request, user *User, session *Session) {
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
			userCanViewRepo := true
			if element.Repo != "" && authProvider != nil {
				userCanViewRepo, _ = authProvider.userCanViewRepo(user, element.Repo)
			}
			if userCanViewRepo {
				environment = element
			}
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
				pingResponse.EnvDetails = getEnvUpResponse(environment.Details, session)
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

func info(w http.ResponseWriter, r *http.Request, user *User, session *Session) {
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
	username := envInfoRequest.Username
	password := envInfoRequest.Password
	if username == "" && user != nil && user.AccessToken != "" {
		username = "x-access-token"
		password = user.AccessToken
	}
	var envInfoResponse = &EnvInfoResponse{}
	minienvConfig, err := downloadMinienvConfig(envInfoRequest.Repo, envInfoRequest.Branch, username, password)
	if err != nil {
		log.Print("Error getting minienv config: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
	if minienvConfig.Env != nil {
		envInfoResponse.Env = &EnvInfoResponseEnv{}
		envInfoResponse.Env.Platform = minienvConfig.Env.Platform
		if minienvConfig.Env.Vars != nil {
			var envVars []EnvInfoResponseEnvVar
			for _, configEnvVar := range *minienvConfig.Env.Vars {
				envVar := EnvInfoResponseEnvVar{}
				envVar.Name = configEnvVar.Name
				envVar.DefaultValue = configEnvVar.DefaultValue
				envVars = append(envVars, envVar)
			}
			envInfoResponse.Env.Vars = &envVars
		}
	}

	// return response
	err = json.NewEncoder(w).Encode(envInfoResponse)
	if err != nil {
		log.Print("Error encoding response: ", err)
		http.Error(w, err.Error(), 400)
		return
	}
}

func up(w http.ResponseWriter, r *http.Request, user *User, session *Session) {
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
				envUpResponse = getEnvUpResponse(environment.Details, session)
			}
		}
		if envUpResponse == nil {
			log.Printf("Creating new deployment...")
			// change status to claimed, so the scheduler doesn't think it has stopped when the old repo is shutdown
			environment.Status = StatusClaimed
			username := envUpRequest.Username
			password := envUpRequest.Password
			if username == "" && user != nil && user.AccessToken != "" {
				username = "x-access-token"
				password = user.AccessToken
			}
			details, err := deployEnv(minienvVersion, environment.Id, environment.ClaimToken, nodeNameOverride, nodeHostProtocol, envUpRequest.Repo, envUpRequest.Branch, username, password, envUpRequest.EnvVars, storageDriver, envPvTemplate, envPvcTemplate, envDeploymentTemplate, envServiceTemplate, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err != nil || details == nil {
				log.Print("Error creating deployment: ", err)
				http.Error(w, err.Error(), 400)
				return
			} else {
				envUpResponse = getEnvUpResponse(details, session)
				environment.Status = StatusRunning
				environment.Repo = envUpRequest.Repo
				environment.Branch = envUpRequest.Branch
				environment.Details = details
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

func getEnvUpResponse(details *DeploymentDetails, session *Session) (*EnvUpResponse) {
	sessionIdStr := ""
	if session != nil {
		sessionIdStr = session.Id
		session.EnvId = details.EnvId
		session.EnvServiceName = getEnvServiceName(details.EnvId, details.ClaimToken)
		sessionStore.setSession(session.Id, session)
	}
	sessionIdStr = strconv.FormatInt(int64(time.Now().Unix()), 16) + "-" + sessionIdStr
	envUpResponse := &EnvUpResponse{}
	envUpResponse.LogUrl = strings.Replace(details.LogUrl, "$sessionId", sessionIdStr, -1)
	envUpResponse.EditorUrl = strings.Replace(details.EditorUrl, "$sessionId", sessionIdStr, -1)
	envUpResponse.Tabs = []Tab{}
	if details.Tabs != nil {
		for _, element := range *details.Tabs {
			tab := Tab{
				Port: element.Port,
				Url: strings.Replace(element.Url, "$sessionId", sessionIdStr, -1),
				Name: element.Name,
				Path: element.Path,
			}
			envUpResponse.Tabs = append(envUpResponse.Tabs, tab)
		}
	}
	return envUpResponse
}

//func isFileInRepo(gitRepo string, gitBranch string, file string) (bool) {
//	url := fmt.Sprintf("%s/raw/%s/%s", gitRepo, gitBranch, file)
//	client := getHttpClient()
//	req, err := http.NewRequest("GET", url, nil)
//	res, err := client.Do(req)
//	if err != nil || res.StatusCode == 404 {
//		return false
//	} else {
//		return true
//	}
//}

func loadFile(fp string) string {
	b, err := ioutil.ReadFile(fp) // just pass the file name
	if err != nil {
		log.Fatalf("Cannot read file")
	}
	return string(b)
}

func authorizeThenServe(handler AuthHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authProvider == nil {
			handler(w, r, nil, nil)
			return
		}
		sessionId := r.Header.Get("Minienv-Session-Id")
		accessToken := r.Header.Get("X-Access-Token")
		if sessionId == "" && accessToken == "" {
			http.Error(w, "Not authenticated", 401)
			return
		}
		var user *User = nil
		var session *Session = nil
		if accessToken != "" {
			user, _ := userStore.getUser(accessToken)
			if user == nil {
				user, err := authProvider.loginUser(accessToken)
				if err != nil {
					http.Error(w, "Not authenticated", 401)
					return
				}
				userStore.setUser(accessToken, user)
			}
		} else {
			session, _ = sessionStore.getSession(sessionId)
			if session == nil || session.User == nil {
				http.Error(w, "Not authenticated", 401)
				return
			}
			user = session.User
		}
		handler(w, r, user, session)
	}
}

func addCorsAndCacheHeadersThenServe(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Add("Access-Control-Allow-Headers", "Minienv-Session-Id")
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
		//uuid, _ := uuid.NewRandom()
		//environmentId := strings.Replace(uuid.String(), "-", "", -1)
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
				getDeploymentResp.Spec.Template.Metadata.Annotations.RepoWithCreds != "" &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken != "" &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.EnvDetails != "" {
				log.Printf("Loading environment %s from deployment metadata.\n", environment.Id)
				running = true
				details  := deploymentDetailsFromString(getDeploymentResp.Spec.Template.Metadata.Annotations.EnvDetails)
				environment.Status = StatusRunning
				environment.ClaimToken = getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken
				environment.LastActivity = time.Now().Unix()
				environment.Repo = getDeploymentResp.Spec.Template.Metadata.Annotations.Repo
				environment.RepoWithCreds = getDeploymentResp.Spec.Template.Metadata.Annotations.RepoWithCreds
				environment.Branch = getDeploymentResp.Spec.Template.Metadata.Annotations.Branch
				environment.Details = details
			} else {
				log.Printf("Insufficient deployment metadata for environment %s.\n", environment.Id)
				deleteEnv(environment.Id, environment.ClaimToken, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
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
			// get the deployment in order to find the claim token
			getDeploymentResp, err := getEnvDeployment(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			if err == nil &&
				getDeploymentResp != nil &&
				getDeploymentResp.Spec != nil &&
				getDeploymentResp.Spec.Template != nil &&
				getDeploymentResp.Spec.Template.Metadata != nil &&
				getDeploymentResp.Spec.Template.Metadata.Annotations != nil &&
				getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken != "" {
				claimToken := getDeploymentResp.Spec.Template.Metadata.Annotations.ClaimToken
				deleteEnv(envId, claimToken, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			} else {
				// we still want to call deleteEnv to tear down pvs, etc
				deleteEnv(envId, "", kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
			}
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
				claimToken := environment.ClaimToken
				environment.Status = StatusIdle
				environment.ClaimToken = ""
				environment.LastActivity = 0
				environment.Repo = ""
				environment.Branch = ""
				environment.Details = nil
				deleteEnv(environment.Id, claimToken, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
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
	githubClientId := os.Getenv("MINIENV_GITHUB_CLIENT_ID")
	githubClientSecret := os.Getenv("MINIENV_GITHUB_CLIENT_SECRET")
	if githubClientId != "" && githubClientSecret != "" {
		authProvider = GitHubAuthProvider{
			ClientId: githubClientId,
			ClientSecret: githubClientSecret,
		}
	}
	redisAddress := os.Getenv("MINIENV_REDIS_ADDRESS")
	redisPassword := os.Getenv("MINIENV_REDIS_PASSWORD")
	redisDb := os.Getenv("MINIENV_REDIS_DB")
	if redisAddress != "" {
		redisSessionStore, err := NewRedisSessionStore(redisAddress, redisPassword, redisDb)
		if err != nil {
			sessionStore = nil
		} else {
			sessionStore = redisSessionStore
		}
	}
	if sessionStore == nil {
		sessionStore = NewInMemorySessionStore()
	}
	userStore = NewInMemoryUserStore()
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
	nodeHostProtocol = os.Getenv("MINIENV_NODE_HOST_PROTOCOL")
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
	http.HandleFunc("/auth/callback", addCorsAndCacheHeadersThenServe(authCallback))
	http.HandleFunc("/me", addCorsAndCacheHeadersThenServe(me))
	http.HandleFunc("/claim", addCorsAndCacheHeadersThenServe(authorizeThenServe(claim)))
	http.HandleFunc("/ping", addCorsAndCacheHeadersThenServe(authorizeThenServe(ping)))
	http.HandleFunc("/info", addCorsAndCacheHeadersThenServe(authorizeThenServe(info)))
	http.HandleFunc("/up", addCorsAndCacheHeadersThenServe(authorizeThenServe(up)))
	http.HandleFunc("/whitelist", addCorsAndCacheHeadersThenServe(authorizeThenServe(whitelist)))
	err := http.ListenAndServe(":"+os.Args[1], nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
