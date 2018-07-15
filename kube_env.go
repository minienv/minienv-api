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

var VAR_MINIENV_NODE_NAME_OVERRIDE = "$minienvNodeNameOverride"
var VAR_MINIENV_VERSION = "$minienvVersion"
var VAR_PV_NAME = "$pvName"
var VAR_PV_SIZE = "$pvSize"
var VAR_PV_PATH = "$pvPath"
var VAR_PVC_NAME = "$pvcName"
var VAR_PVC_STORAGE_CLASS = "$pvcStorageClass"
var VAR_SERVICE_NAME = "$serviceName"
var VAR_DEPLOYMENT_NAME = "$deploymentName"
var VAR_APP_LABEL = "$appLabel"
var VAR_CLAIM_TOKEN = "$claimToken"
var VAR_GIT_REPO = "$gitRepo"
var VAR_GIT_BRANCH = "$gitBranch"
var VAR_ENV_DETAILS = "$envDetails"
var VAR_ENV_VARS = "$envVars"
var VAR_STORAGE_DRIVER = "$storageDriver"
var VAR_LOG_PORT = "$logPort"
var VAR_EDITOR_PORT = "$editorPort"
var VAR_PROXY_PORT = "$proxyPort"
var VAR_ALLOW_ORIGIN = "$allowOrigin"

var DEFAULT_LOG_PORT = "30081"
var DEFAULT_EDITOR_PORT = "30082"
var DEFAULT_PROXY_PORT = "30083"

type MinienvConfig struct {
	Env *MinienvConfigEnv `json:"env"`
	Editor *MinienvConfigEditor `json:"editor"`
	Proxy *MinienvConfigProxy `json:"proxy"`
}

type MinienvConfigEnv struct {
	Vars *[]MinienvConfigEnvVar `json:"vars"`
}

type MinienvConfigEnvVar struct {
	Name string `json:"name"`
	DefaultValue string `json:"defaultValue"`
}

type MinienvConfigEditor struct {
	Hide bool `json:"hide"`
	SrcDir string `json:"srcDir"`
}

type MinienvConfigProxy struct {
	Ports *[]MinienvConfigProxyPort `json:"ports"`
}

type MinienvConfigProxyPort struct {
	Port int `json:"port"`
	Hide bool `json:"hide"`
	Name string `json:"name"`
	Path string `json:"path"`
	Tabs *[]MinienvConfigProxyPortTab `json:"tabs"`
}

type MinienvConfigProxyPortTab struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Tab struct {
	Port int `json:"port"`
	Url string `json:"url"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type DeploymentDetails struct {
	NodeHostName string
	LogPort int
	LogUrl string
	EditorPort int
	EditorUrl string
	ProxyPort int
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

func deleteEnv(envId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) {
	log.Printf("Deleting env %s...\n", envId)
	deploymentName := getEnvDeploymentName(envId)
	appLabel := getEnvAppLabel(envId)
	serviceName := getEnvServiceName(envId)
	_, _ = deleteDeployment(deploymentName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteReplicaSet(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteService(serviceName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = waitForPodTermination(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
}

func downloadMinienvConfig(gitRepo string, gitBranch string) (MinienvConfig, error) {
	// download minienv.json
	var minienvConfig MinienvConfig
	minienvConfigUrl := fmt.Sprintf("%s/raw/%s/minienv.json", gitRepo, gitBranch)
	log.Printf("Downloading minienv config from '%s'...\n", minienvConfigUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", minienvConfigUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error downloading minienv config: ", err)
	} else {
		err = json.NewDecoder(resp.Body).Decode(&minienvConfig)
		if err != nil {
			return minienvConfig, err
		}
	}
	return minienvConfig, nil
}

func deployEnv(minienvVersion string, envId string, claimToken string, nodeNameOverride string, gitRepo string, gitBranch string, envVars map[string]string, storageDriver string, pvTemplate string, pvcTemplate string, deploymentTemplate string, serviceTemplate string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*DeploymentDetails, error) {
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
	deleteEnv(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	// download minienv.json
	minienvConfig, err := downloadMinienvConfig(gitRepo, gitBranch)
	if err != nil {
		log.Println("Error downloading minienv.json", err)
		return nil, err
	}
	// download docker-compose file (first try yml, then yaml)
	tabs := []*Tab{}
	dockerComposeUrl := fmt.Sprintf("%s/raw/master/docker-compose.yml", gitRepo)
	log.Printf("Downloading docker-compose file from '%s'...\n", dockerComposeUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", dockerComposeUrl, nil)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		log.Println("Error downloading docker-compose.yml: ", err)
		dockerComposeUrl := fmt.Sprintf("%s/raw/master/docker-compose.yaml", gitRepo)
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
	// populate docker compose names and paths
	if minienvConfig.Proxy != nil && minienvConfig.Proxy.Ports != nil && len(*minienvConfig.Proxy.Ports) > 0 {
		for _, proxyPort := range *minienvConfig.Proxy.Ports {
			if proxyPort.Hide == true {
				// ignore
			} else if proxyPort.Tabs != nil && len(*proxyPort.Tabs) > 0 {
				for i, proxyTab := range *proxyPort.Tabs {
					if i == 0 {
						// update the original docker compose port
						for _, tab := range tabs {
							if tab.Port == proxyPort.Port {
								if proxyTab.Name != "" {
									tab.Name = proxyTab.Name
								}
								if proxyTab.Path != "" {
									tab.Path = proxyTab.Path
								}
							}
						}
					} else {
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
			} else {
				for _, tab := range tabs {
					if tab.Port == proxyPort.Port {
						if proxyPort.Name != "" {
							tab.Name = proxyPort.Name
						}
						if proxyPort.Path != "" {
							tab.Path = proxyPort.Path
						}
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
			pv = strings.Replace(pv, VAR_PV_NAME, pvName, -1)
			pv = strings.Replace(pv, VAR_PV_PATH, pvPath, -1)
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
		pvc = strings.Replace(pvc, VAR_PV_SIZE, provisionVolumeSize, -1)
		pvc = strings.Replace(pvc, VAR_PVC_NAME, pvcName, -1)
		pvc = strings.Replace(pvc, VAR_PVC_STORAGE_CLASS, envPvcStorageClass, -1)
		_, err = savePersistentVolumeClaim(pvc, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error saving persistent volume claim: ", err)
			return nil, err
		}
	}
	// create the service first - we need the ports to serialize the details with the deployment
	appLabel := getEnvAppLabel(envId)
	serviceName := getEnvServiceName(envId)
	service := serviceTemplate
	service = strings.Replace(service, VAR_SERVICE_NAME, serviceName, -1)
	service = strings.Replace(service, VAR_APP_LABEL, appLabel, -1)
	service = strings.Replace(service, VAR_LOG_PORT, DEFAULT_LOG_PORT, -1)
	service = strings.Replace(service, VAR_EDITOR_PORT, DEFAULT_EDITOR_PORT, -1)
	service = strings.Replace(service, VAR_PROXY_PORT, DEFAULT_PROXY_PORT, -1)
	serviceResp, err := saveService(service, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving service: ", err)
		return nil, err
	}
	// get the deployment details - we will serialize them with the deployment
	logNodePort := 0
	editorNodePort := 0
	proxyNodePort := 0
	for _, element := range serviceResp.Spec.Ports {
		if element.Name == "log" {
			logNodePort = element.NodePort
		}
		if element.Name == "editor" {
			editorNodePort = element.NodePort
		}
		if element.Name == "proxy" {
			proxyNodePort = element.NodePort
		}
	}
	details := &DeploymentDetails{}
	details.NodeHostName = os.Getenv("MINIENV_NODE_HOST_NAME") // mw:TODO
	details.LogPort = logNodePort
	details.LogUrl = fmt.Sprintf("http://%s:%d", details.NodeHostName, details.LogPort)
	details.EditorPort = editorNodePort
	details.EditorUrl = fmt.Sprintf("http://%s:%d", details.NodeHostName, details.EditorPort)
	if minienvConfig.Editor != nil {
		if minienvConfig.Editor.Hide {
			details.EditorPort = 0
			details.EditorUrl = ""
		} else if minienvConfig.Editor.SrcDir != "" {
			details.EditorUrl += "?src=" + url.QueryEscape(minienvConfig.Editor.SrcDir)
		}
	}
	details.ProxyPort = proxyNodePort
	for _, tab := range tabs {
		tab.Url = fmt.Sprintf("http://%d.%s:%d%s",tab.Port,details.NodeHostName,details.ProxyPort,tab.Path)
	}
	details.Tabs = &tabs
	// create the deployment
	// TODO: Check if default ports are going to work and if not change them
	deploymentName := getEnvDeploymentName(envId)
	deploymentDetailsStr := deploymentDetailsToString(details)
	deployment := deploymentTemplate
	deployment = strings.Replace(deployment, VAR_MINIENV_NODE_NAME_OVERRIDE, nodeNameOverride, -1)
	deployment = strings.Replace(deployment, VAR_MINIENV_VERSION, minienvVersion, -1)
	deployment = strings.Replace(deployment, VAR_DEPLOYMENT_NAME, deploymentName, -1)
	deployment = strings.Replace(deployment, VAR_APP_LABEL, appLabel, -1)
	deployment = strings.Replace(deployment, VAR_CLAIM_TOKEN, claimToken, -1)
	deployment = strings.Replace(deployment, VAR_GIT_REPO, gitRepo, -1)
	deployment = strings.Replace(deployment, VAR_GIT_BRANCH, gitBranch, -1)
	deployment = strings.Replace(deployment, VAR_ENV_DETAILS, deploymentDetailsStr, -1)
	deployment = strings.Replace(deployment, VAR_ENV_VARS, envVarsYaml, -1)
	deployment = strings.Replace(deployment, VAR_STORAGE_DRIVER, storageDriver, -1)
	deployment = strings.Replace(deployment, VAR_LOG_PORT, DEFAULT_LOG_PORT, -1)
	deployment = strings.Replace(deployment, VAR_EDITOR_PORT, DEFAULT_EDITOR_PORT, -1)
	deployment = strings.Replace(deployment, VAR_PROXY_PORT, DEFAULT_PROXY_PORT, -1)
	deployment = strings.Replace(deployment, VAR_ALLOW_ORIGIN, allowOrigin, -1)
	deployment = strings.Replace(deployment, VAR_PVC_NAME, pvcName, -1)
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

func getEnvServiceName(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-service", envId))
}

func getEnvDeploymentName(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-deployment", envId))
}

func getEnvAppLabel(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-deployment", envId))
}