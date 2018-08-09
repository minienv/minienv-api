package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

var NodeHostName = os.Getenv("MINIENV_NODE_HOST_NAME")
var NodeHostProtocol = os.Getenv("MINIENV_NODE_HOST_PROTOCOL")

var VarMinienvPlatform = "$minienvPlatform"
var VarMinienvPlatformPort = "$minienvPlatformPort"
var VarMinienvNodeNameOverride = "$minienvNodeNameOverride"
var VarMinienvNodeHostProtocol = "$minienvNodeHostProtocol"
var VarMinienvVersion = "$minienvVersion"
var VarPvName = "$pvName"
var VarPvSize = "$pvSize"
var VarPvPath = "$pvPath"
var VarPvcName = "$pvcName"
var VarPvcStorageClass = "$pvcStorageClass"
var VarServiceName = "$serviceName"
var VarDeploymentName = "$deploymentName"
var VarAppLabel = "$appLabel"
var VarClaimToken = "$claimToken"
var VarGitRepoWithCreds = "$gitRepoWithCreds"
var VarGitRepo = "$gitRepo"
var VarGitBranch = "$gitBranch"
var VarEnvDetails = "$envDetails"
var VarEnvVars = "$envVars"
var VarStorageDriver = "$storageDriver"
var VarLogPort = "$logPort"
var VarEditorPort = "$editorPort"
var VarProxyPort = "$proxyPort"
var VarAllowOrigin = "$allowOrigin"

var DefaultLogPort = "8001" //"30081"
var DefaultEditorPort = "8002" //"30082"
var DefaultProxyPort = "8003" //"30083"

type MinienvGitHubConfig struct {
	Env string `yaml:"env"`
	Disabled bool `yaml:"disabled`
	Expiration int `yaml:"expiration`
	Metadata *MinienvConfig `yaml:"metadata`
}

type MinienvConfig struct {
	Env *MinienvConfigEnv `json:"env" yaml:"env"`
	Editor *MinienvConfigEditor `json:"editor" yaml:"editor`
	Proxy *MinienvConfigProxy `json:"proxy" yaml:"proxy`
}

type MinienvConfigEnv struct {
	Platform string `json:"platform" yaml:"platform"`
	Vars *[]MinienvConfigEnvVar `json:"vars" yaml:"vars"`
}

type MinienvConfigEnvVar struct {
	Name string `json:"name" yaml:"name"`
	DefaultValue string `json:"defaultValue" yaml:"defaultValue"`
}

type MinienvConfigEditor struct {
	Hide bool `json:"hide" yaml:"hide"`
	SrcDir string `json:"srcDir" yaml:"srcDir"`
}

type MinienvConfigProxy struct {
	Ports *[]MinienvConfigProxyPort `json:"ports" yaml:"ports"`
}

type MinienvConfigProxyPort struct {
	Port int `json:"port" yaml:"port"`
	Hide bool `json:"hide" yaml:"hide"`
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
	Tabs *[]MinienvConfigProxyPortTab `json:"tabs" yaml:"tabs"`
}

type MinienvConfigProxyPortTab struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

type Tab struct {
	Port int `json:"port" yaml:"port"`
	Url string `json:"url" yaml:"url"`
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
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

func downloadMinienvGitHubConfig(gitRepo string, gitBranch string, gitUsername string, gitPassword string) (*MinienvGitHubConfig, error) {
	// download .github/minienv.yml
	minienvGitHubConfigUrl := getDownloadUrl(".github/minienv.yml", gitRepo, gitBranch, gitUsername, gitPassword)
	log.Printf("Downloading minienv config from '%s'...\n", minienvGitHubConfigUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", minienvGitHubConfigUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	} else if resp.StatusCode == 200 {
		var minienvGithubConfig MinienvGitHubConfig
		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error downloading minienv.yml file: ", err)
			return nil, err
		}
		err = yaml.Unmarshal(data, &minienvGithubConfig)
		if err != nil {
			log.Println("Error parsing minienv.yml file: ", err)
			return nil, err
		} else {
			return &minienvGithubConfig, nil
		}
	} else {
		return nil, nil
	}
}

func downloadMinienvConfig(gitRepo string, gitBranch string, gitUsername string, gitPassword string) (*MinienvConfig, error) {
	// download minienv.json
	minienvGitHubConfig, err := downloadMinienvGitHubConfig(gitRepo, gitBranch, gitUsername, gitPassword)
	if minienvGitHubConfig != nil && minienvGitHubConfig.Metadata != nil {
		return minienvGitHubConfig.Metadata, err
	}
	minienvConfigUrl := getDownloadUrl("minienv.json", gitRepo, gitBranch, gitUsername, gitPassword)
	log.Printf("Downloading minienv.json from '%s'...\n", minienvConfigUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", minienvConfigUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	} else if resp.StatusCode == 200 {
		var minienvConfig MinienvConfig
		err = json.NewDecoder(resp.Body).Decode(&minienvConfig)
		if err != nil {
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
	// download minienv.json
	minienvConfig, err := downloadMinienvConfig(gitRepo, gitBranch, gitUsername, gitPassword)
	if err != nil {
		log.Println("Error downloading minienv.json", err)
	}
	var tabs []*Tab
	if minienvConfig == nil || minienvConfig.Env == nil || minienvConfig.Env.Platform == "" {
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
	if minienvConfig.Proxy != nil && minienvConfig.Proxy.Ports != nil && len(*minienvConfig.Proxy.Ports) > 0 {
		for _, proxyPort := range *minienvConfig.Proxy.Ports {
			if proxyPort.Hide == true {
				continue
			}
			// user can specify one or more tabs for any port
			// if they only specify one then
			var configProxyTabs []MinienvConfigProxyPortTab
			if proxyPort.Tabs != nil && len(*proxyPort.Tabs) > 0 {
				configProxyTabs = *proxyPort.Tabs
			} else {
				configProxyTabs = make([]MinienvConfigProxyPortTab, 1)
				configProxyTabs[0] = MinienvConfigProxyPortTab{
					Name: proxyPort.Name,
					Path: proxyPort.Path,
				}
			}
			for i, proxyTab := range configProxyTabs {
				tabUpdated := false
				if i == 0 {
					// update the original docker compose port if it exists
					for _, tab := range tabs {
						if tab.Port == proxyPort.Port {
							if proxyTab.Name != "" {
								tab.Name = proxyTab.Name
							}
							if proxyTab.Path != "" {
								tab.Path = proxyTab.Path
							}
							tabUpdated = true
							break
						}
					}
				}
				if ! tabUpdated {
					// add other docker compose ports
					tab := &Tab{}
					tab.Port = proxyPort.Port
					tab.Name = strconv.Itoa(proxyPort.Port)
					tabs = append(tabs, tab)
					if proxyTab.Name != "" {
						tab.Name = proxyTab.Name
					}
					if proxyTab.Path != "" {
						tab.Path = proxyTab.Path
					}
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
	if minienvConfig.Editor != nil {
		if minienvConfig.Editor.Hide {
			details.EditorUrl = ""
		} else if minienvConfig.Editor.SrcDir != "" {
			details.EditorUrl += "?src=" + url.QueryEscape(minienvConfig.Editor.SrcDir)
		}
	}
	for _, tab := range tabs {
		tab.Url = fmt.Sprintf("%s://%s-%s-%d.%s%s", NodeHostProtocol, "$sessionId", proxyPort, tab.Port, details.NodeHostName, tab.Path)
	}
	details.Tabs = &tabs
	// create the deployment
	platform := ""
	platformPort := ""
	if minienvConfig != nil && minienvConfig.Env != nil && minienvConfig.Env.Platform != "" {
		platform = minienvConfig.Env.Platform
		if len(tabs) > 0 {
			platformPort = strconv.Itoa(tabs[0].Port)
		}
	}
	gitRepoWithCreds := getUrlWithCredentials(gitRepo, gitUsername, gitPassword)
	deploymentName := getEnvDeploymentName(envId)
	deploymentDetailsStr := deploymentDetailsToString(details)
	deployment := deploymentTemplate
	deployment = strings.Replace(deployment, VarMinienvPlatformPort, platformPort, -1) // this must be replaced before VarMinienvPlatform
	deployment = strings.Replace(deployment, VarMinienvPlatform, platform, -1)
	deployment = strings.Replace(deployment, VarMinienvNodeNameOverride, nodeNameOverride, -1)
	deployment = strings.Replace(deployment, VarMinienvNodeHostProtocol, nodeHostProtocol, -1)
	deployment = strings.Replace(deployment, VarMinienvVersion, minienvVersion, -1)
	deployment = strings.Replace(deployment, VarDeploymentName, deploymentName, -1)
	deployment = strings.Replace(deployment, VarAppLabel, appLabel, -1)
	deployment = strings.Replace(deployment, VarClaimToken, claimToken, -1)
	// make sure this replace is done before gitRepo
	deployment = strings.Replace(deployment, VarGitRepoWithCreds, gitRepoWithCreds, -1)
	deployment = strings.Replace(deployment, VarGitRepo, gitRepo, -1)
	deployment = strings.Replace(deployment, VarGitBranch, gitBranch, -1)
	deployment = strings.Replace(deployment, VarEnvDetails, deploymentDetailsStr, -1)
	deployment = strings.Replace(deployment, VarEnvVars, envVarsYaml, -1)
	deployment = strings.Replace(deployment, VarStorageDriver, storageDriver, -1)
	deployment = strings.Replace(deployment, VarLogPort, DefaultLogPort, -1)
	deployment = strings.Replace(deployment, VarEditorPort, DefaultEditorPort, -1)
	deployment = strings.Replace(deployment, VarProxyPort, DefaultProxyPort, -1)
	deployment = strings.Replace(deployment, VarAllowOrigin, allowOrigin, -1)
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