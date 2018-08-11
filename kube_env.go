package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

var NodeHostName = os.Getenv("MINIENV_NODE_HOST_NAME")
var NodeHostProtocol = os.Getenv("MINIENV_NODE_HOST_PROTOCOL")

var VarMinienvVersion = "$minienvVersion"
var VarMinienvNodeNameOverride = "$nodeNameOverride"
var VarMinienvNodeHostProtocol = "$nodeHostProtocol"
var VarAllowOrigin = "$allowOrigin"
var VarStorageDriver = "$storageDriver"
var VarMinienvPlatform = "$platform"
var VarMinienvPlatformCommand = "$platformCommand"
var VarMinienvPlatformPort = "$platformPort"
var VarGitRepo = "$gitRepo"
var VarGitRepoWithCreds = "$gitRepoWithCreds"
var VarGitBranch = "$gitBranch"
var VarProxyPort = "$proxyPort"
var VarLogPort = "$logPort"
var VarEditorPort = "$editorPort"
var VarEditorSrcDir = "$editorSrcDir"
var VarPvName = "$pvName"
var VarPvSize = "$pvSize"
var VarPvPath = "$pvPath"
var VarPvcName = "$pvcName"
var VarPvcStorageClass = "$pvcStorageClass"
var VarServiceName = "$serviceName"
var VarDeploymentName = "$deploymentName"
var VarAppLabel = "$appLabel"
var VarClaimToken = "$claimToken"
var VarEnvDetails = "$envDetails"
var VarEnvVars = "$envVars"

var DefaultLogPort = "8001" //"30081"
var DefaultEditorPort = "8002" //"30082"
var DefaultProxyPort = "8003" //"30083"

type MinienvConfig struct {
	Env string                    `yaml:"env"`
	Disabled bool                 `yaml:"disabled"`
	Expiration int                `yaml:"expiration"`
	Runtime *MinienvConfigRuntime `yaml:"runtime"`
	Metadata *MinienvConfigMeta   `yaml:"metadata"`
}

type MinienvConfigRuntime struct {
	Platform string `yaml:"platform"`
	Port int `yaml:"port"`
	Command string `yaml:"command"`
}

type MinienvConfigMeta struct {
	Env       *MinienvConfigMetaEnv       `yaml:"env"`
	EditorTab *MinienvConfigMetaEditorTab `yaml:"editorTab"`
	AppTabs   *[]MinienvConfigMetaAppTab    `yaml:"appTabs"`
}

type MinienvConfigMetaEnv struct {
	Vars *[]MinienvConfigMetaEnvVar `yaml:"vars"`
}

type MinienvConfigMetaEnvVar struct {
	Name string `yaml:"name"`
	DefaultValue string `yaml:"defaultValue"`
}

type MinienvConfigMetaEditorTab struct {
	Hide bool `yaml:"hide"`
	SrcDir string `yaml:"srcDir"`
}

type MinienvConfigMetaAppTab struct {
	Port int    `yaml:"port"`
	Hide bool   `yaml:"hide"`
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

type Tab struct {
	Port int `json:"port"`
	Url string `json:"url"`
	Hide bool   `json:"hide"`
	Name string `json:"name"`
	Path string `json:"path"`
	AppTab *MinienvConfigMetaAppTab
}

type DeploymentDetails struct {
	NodeHostName string
	EnvId string
	ClaimToken string
	LogUrl string
	EditorUrl string
	Tabs *[]*Tab
}

func getEnvDeployment(envId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetDeploymentResponse, error) {
	return getDeployment(getEnvDeploymentName(envId), kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
}

func isEnvDeployed(envId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	getDeploymentResp, err := getDeployment(getEnvDeploymentName(envId), kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		return false, err
	} else {
		return getDeploymentResp != nil, nil
	}
}

func deleteEnv(envId string, claimToken string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) {
	log.Printf("Deleting env %s...\n", envId)
	deploymentName := getEnvDeploymentName(envId)
	appLabel := getEnvAppLabel(envId, claimToken)
	serviceName := getEnvServiceName(envId, claimToken)
	_, _ = deleteDeployment(deploymentName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteReplicaSet(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteService(serviceName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = waitForPodTermination(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
}

func getUrlWithCredentials(url string, username string, password string) (string) {
	if username != "" && password != "" {
		url = strings.Replace(url, "https://", fmt.Sprintf("https://%s:%s@", username, password), 1)
		url = strings.Replace(url, "http://", fmt.Sprintf("http://%s:%s@", username, password), 1)
	}
	return url
}

func getDownloadUrl(path string, gitRepo string, gitBranch string, gitUsername string, gitPassword string) (string) {
	url := fmt.Sprintf("%s/%s/%s", gitRepo, gitBranch, path)
	url = strings.Replace(url, "github.com", "raw.githubusercontent.com", 1)
	url = getUrlWithCredentials(url, gitUsername, gitPassword)
	return url
}

func downloadMinienvConfig(gitRepo string, gitBranch string, gitUsername string, gitPassword string) (*MinienvConfig, error) {
	// download .github/minienv.yml
	minienvConfigUrl := getDownloadUrl(".github/minienv.yml", gitRepo, gitBranch, gitUsername, gitPassword)
	log.Printf("Downloading minienv config from '%s'...\n", minienvConfigUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", minienvConfigUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	} else if resp.StatusCode == 200 {
		var minienvConfig MinienvConfig
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error downloading minienv.yml file: ", err)
			return nil, err
		}
		err = yaml.Unmarshal(data, &minienvConfig)
		if err != nil {
			log.Println("Error parsing minienv.yml file: ", err)
			return nil, err
		} else {
			return &minienvConfig, nil
		}
	} else {
		return nil, nil
	}
}

func deployEnv(minienvVersion string, envId string, claimToken string, nodeNameOverride string, nodeHostProtocol string, gitRepo string, gitBranch string, gitUsername string, gitPassword string, envVars map[string]string, storageDriver string, pvTemplate string, pvcTemplate string, deploymentTemplate string, serviceTemplate string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*DeploymentDetails, error) {
	envVarsYaml := ""
	if envVars != nil {
		first := true
		for k, v := range envVars {
			if ! first {
				envVarsYaml += "\n"
			} else {
				first = false
			}
			envVarsYaml += "          - name: " + k
			envVarsYaml += "\n            value: \"" + v + "\""
		}
	}
	// delete env, if it exists
	deleteEnv(envId, claimToken, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	// download minienv config
	minienvConfig, err := downloadMinienvConfig(gitRepo, gitBranch, gitUsername, gitPassword)
	if err != nil {
		log.Println("Error downloading minienv.json", err)
	}
	var tabs []*Tab
	if minienvConfig == nil || minienvConfig.Runtime == nil || minienvConfig.Runtime.Platform == "" {
		// download docker-compose file if platform not specified in minienv config
		// first try yml, then yaml
		dockerComposeUrl := getDownloadUrl("docker-compose.yml", gitRepo, gitBranch, gitUsername, gitPassword)
		log.Printf("Downloading docker-compose file from '%s'...\n", dockerComposeUrl)
		client := getHttpClient()
		req, err := http.NewRequest("GET", dockerComposeUrl, nil)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			log.Println("Error downloading docker-compose.yml: ", err)
			dockerComposeUrl := getDownloadUrl("docker-compose.yaml", gitRepo, gitBranch, gitUsername, gitPassword)
			req, err = http.NewRequest("GET", dockerComposeUrl, nil)
			resp, err = client.Do(req)
			if err != nil || resp.StatusCode != 200 {
				log.Println("Error downloading docker-compose.yaml: ", err)
				return nil, err
			}
		}
		m := make(map[interface{}]interface{})
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error downloading docker-compose file: ", err)
			return nil, err
		} else {
			err = yaml.Unmarshal(data, &m)
			if err != nil {
				log.Println("Error parsing docker-compose file: ", err)
				return nil, err
			} else {
				for k, v := range m {
					populateTabs(v, &tabs, k.(string))
				}
			}
		}
	}
	// populate docker compose names and paths
	if minienvConfig.Metadata != nil && minienvConfig.Metadata.AppTabs != nil && len(*minienvConfig.Metadata.AppTabs) > 0 {
		for _, appTab := range *minienvConfig.Metadata.AppTabs {
			tabUpdated := false
			// update the original docker compose port if it exists
			for _, tab := range tabs {
				if tab.Port == appTab.Port && tab.AppTab == nil {
					tab.AppTab = &appTab
					tab.Hide = appTab.Hide
					if appTab.Name != "" {
						tab.Name = appTab.Name
					}
					if appTab.Path != "" {
						tab.Path = appTab.Path
					}
					tabUpdated = true
					break
				}
			}
			if ! tabUpdated {
				// add other docker compose ports
				tab := &Tab{}
				tab.AppTab = &appTab
				tab.Hide = appTab.Hide
				tab.Port = appTab.Port
				tab.Name = strconv.Itoa(appTab.Port)
				tabs = append(tabs, tab)
				if appTab.Name != "" {
					tab.Name = appTab.Name
				}
				if appTab.Path != "" {
					tab.Path = appTab.Path
				}
			}
		}
	}
	// create persistent volume if using host paths
	if envPvHostPath {
		pvName := getPersistentVolumeName(envId)
		pvPath := getPersistentVolumePath(envId)
		pvResponse, err := getPersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
		if err != nil {
			log.Println("Error getting persistent volume: ", err)
			return nil, err
		} else if pvResponse == nil {
			pv := pvTemplate
			pv = strings.Replace(pv, VarPvName, pvName, -1)
			pv = strings.Replace(pv, VarPvPath, pvPath, -1)
			_, err = savePersistentVolume(pv, kubeServiceToken, kubeServiceBaseUrl)
			if err != nil {
				log.Println("Error saving persistent volume: ", err)
				return nil, err
			}
		}
	}
	// create persistent volume claim, if not exists
	pvcName := getPersistentVolumeClaimName(envId)
	pvcResponse, err := getPersistentVolumeClaim(pvcName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error getting persistent volume claim: ", err)
		return nil, err
	} else if pvcResponse == nil {
		pvc := pvcTemplate
		pvc = strings.Replace(pvc, VarPvSize, provisionVolumeSize, -1)
		pvc = strings.Replace(pvc, VarPvcName, pvcName, -1)
		pvc = strings.Replace(pvc, VarPvcStorageClass, envPvcStorageClass, -1)
		_, err = savePersistentVolumeClaim(pvc, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error saving persistent volume claim: ", err)
			return nil, err
		}
	}
	// ports
	logPort := DefaultLogPort
	editorPort := DefaultEditorPort
	proxyPort := DefaultProxyPort
	// create the service first - we need the ports to serialize the details with the deployment
	appLabel := getEnvAppLabel(envId, claimToken)
	serviceName := getEnvServiceName(envId, claimToken)
	service := serviceTemplate
	service = strings.Replace(service, VarServiceName, serviceName, -1)
	service = strings.Replace(service, VarAppLabel, appLabel, -1)
	service = strings.Replace(service, VarLogPort, logPort, -1)
	service = strings.Replace(service, VarEditorPort, editorPort, -1)
	service = strings.Replace(service, VarProxyPort, proxyPort, -1)
	_, err = saveService(service, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving service: ", err)
		return nil, err
	}
	details := &DeploymentDetails{}
	details.NodeHostName = NodeHostName
	details.EnvId = envId
	details.ClaimToken = claimToken
	details.LogUrl = fmt.Sprintf("%s://%s-%s.%s", NodeHostProtocol, "$sessionId", logPort, details.NodeHostName)
	details.EditorUrl = fmt.Sprintf("%s://%s-%s.%s", NodeHostProtocol, "$sessionId", editorPort, details.NodeHostName)
	if minienvConfig.Metadata != nil && minienvConfig.Metadata.EditorTab != nil {
		if minienvConfig.Metadata.EditorTab.Hide {
			details.EditorUrl = ""
		}
	}
	// add tab based on port if no tabs already provided
	if len(tabs) == 0 && minienvConfig != nil && minienvConfig.Runtime != nil && minienvConfig.Runtime.Port > 0 {
		tab := &Tab{}
		tab.Hide = false
		tab.Port = minienvConfig.Runtime.Port
		tab.Name = strconv.Itoa(minienvConfig.Runtime.Port)
		tab.Path = "/"
		tabs = append(tabs, tab)
	}
	for _, tab := range tabs {
		tab.Url = fmt.Sprintf("%s://%s-%s-%d.%s%s", NodeHostProtocol, "$sessionId", proxyPort, tab.Port, details.NodeHostName, tab.Path)
	}
	details.Tabs = &tabs
	// create the deployment
	platform := ""
	platformCommand := ""
	platformPort := ""
	editorSrcDir := ""
	if minienvConfig != nil {
		if minienvConfig.Runtime != nil && minienvConfig.Runtime.Platform != "" {
			platform = minienvConfig.Runtime.Platform
			platformCommand = minienvConfig.Runtime.Command
			if minienvConfig.Runtime.Port > 0 {
				platformPort = strconv.Itoa(minienvConfig.Runtime.Port)
			} else if len(tabs) > 0 {
				platformPort = strconv.Itoa(tabs[0].Port)
			}
		}
		if minienvConfig.Metadata != nil && minienvConfig.Metadata.EditorTab != nil {
			editorSrcDir = minienvConfig.Metadata.EditorTab.SrcDir
		}
	}
	gitRepoWithCreds := getUrlWithCredentials(gitRepo, gitUsername, gitPassword)
	deploymentName := getEnvDeploymentName(envId)
	deploymentDetailsStr := deploymentDetailsToString(details)
	deployment := deploymentTemplate
	deployment = strings.Replace(deployment, VarMinienvVersion, minienvVersion, -1)
	deployment = strings.Replace(deployment, VarMinienvNodeNameOverride, nodeNameOverride, -1)
	deployment = strings.Replace(deployment, VarMinienvNodeHostProtocol, nodeHostProtocol, -1)
	deployment = strings.Replace(deployment, VarAllowOrigin, allowOrigin, -1)
	deployment = strings.Replace(deployment, VarStorageDriver, storageDriver, -1)
	deployment = strings.Replace(deployment, VarMinienvPlatformPort, platformPort, -1) // this must be replaced before VarMinienvPlatform
	deployment = strings.Replace(deployment, VarMinienvPlatformCommand, platformCommand, -1) // this must be replaced before VarMinienvPlatform
	deployment = strings.Replace(deployment, VarMinienvPlatform, platform, -1)
	deployment = strings.Replace(deployment, VarGitRepoWithCreds, gitRepoWithCreds, -1) // make sure this replace is done before gitRepo
	deployment = strings.Replace(deployment, VarGitRepo, gitRepo, -1)
	deployment = strings.Replace(deployment, VarGitBranch, gitBranch, -1)
	deployment = strings.Replace(deployment, VarProxyPort, DefaultProxyPort, -1)
	deployment = strings.Replace(deployment, VarLogPort, DefaultLogPort, -1)
	deployment = strings.Replace(deployment, VarEditorPort, DefaultEditorPort, -1)
	deployment = strings.Replace(deployment, VarEditorSrcDir, editorSrcDir, -1)
	deployment = strings.Replace(deployment, VarDeploymentName, deploymentName, -1)
	deployment = strings.Replace(deployment, VarAppLabel, appLabel, -1)
	deployment = strings.Replace(deployment, VarClaimToken, claimToken, -1)
	deployment = strings.Replace(deployment, VarEnvDetails, deploymentDetailsStr, -1)
	deployment = strings.Replace(deployment, VarEnvVars, envVarsYaml, -1)
	deployment = strings.Replace(deployment, VarPvcName, pvcName, -1)
	_, err = saveDeployment(deployment, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving deployment: ", err)
		return nil, err
	}
	// return
	return details, nil
}

func deploymentDetailsToString(details *DeploymentDetails) (string) {
	b, err := json.Marshal(details)
	if err != nil {
		return ""
	}
	s := strings.Replace(string(b), "\"", "\\\"", -1)
	return s
}

func deploymentDetailsFromString(envDetails string) (*DeploymentDetails) {
	envDetails = strings.Replace(envDetails, "\\\"", "\"", -1)
	var deploymentDetails DeploymentDetails
	err := json.Unmarshal([]byte(envDetails), &deploymentDetails)
	if err != nil {
		return nil
	} else {
		return &deploymentDetails
	}
}

func populateTabs(v interface{}, tabs *[]*Tab, parent string) {
	typ := reflect.TypeOf(v).Kind()
	if typ == reflect.String {
		if parent == "ports" {
			portString := strings.SplitN(v.(string), ":", 2)[0]
			port, err := strconv.Atoi(portString)
			if err == nil {
				tab := &Tab{}
				tab.Port = port
				tab.Name = strconv.Itoa(port)
				*tabs = append(*tabs, tab)
			}
		}
	} else if typ == reflect.Slice {
		populateTabsSlice(v.([]interface{}), tabs, parent)
	} else if typ == reflect.Map {
		populateTabsMap(v.(map[interface{}]interface{}), tabs)
	}
}

func populateTabsMap(m map[interface{}]interface{}, tabs *[]*Tab) {
	for k, v := range m {
		populateTabs(v, tabs, strings.ToLower(k.(string)))
	}
}

func populateTabsSlice(slc []interface{}, tabs *[]*Tab, parent string) {
	for _, v := range slc {
		populateTabs(v, tabs, parent)
	}
}

func getPersistentVolumeName(envId string) string {
	return strings.ToLower(fmt.Sprintf("minienv-env-%s-pv", envId))
}

func getPersistentVolumePath(envId string) string {
	return strings.ToLower(fmt.Sprintf("/minienv-env-%s", envId))
}

func getPersistentVolumeClaimName(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-pvc", envId))
}

func getEnvDeploymentName(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-deployment", envId))
}

// service name and app label are based on claim token
// this way users won't mistakenly get access to services and deployments that they shouldn't have access to
func getEnvServiceName(envId string, claimToken string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-service-%s", envId, claimToken))
}

func getEnvAppLabel(envId string, claimToken string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-app-%s", envId, claimToken))
}