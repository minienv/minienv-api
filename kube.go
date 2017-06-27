package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var NAMESPACE = "default"

var VAR_PV_NAME string = "$pvName"
var VAR_PV_PATH string = "$pvPath"
var VAR_PVC_NAME string = "$pvcName"
var VAR_SERVICE_NAME string = "$serviceName"
var VAR_DEPLOYMENT_NAME string = "$deploymentName"
var VAR_APP_LABEL string = "$appLabel"
var VAR_STORAGE_DRIVER string = "$storageDriver"
var VAR_LOG_PORT string = "$logPort"
var VAR_EDITOR_PORT string = "$editorPort"
var VAR_PROXY_PORT string = "$proxyPort"
var VAR_GIT_REPO string = "$gitRepo"
var VAR_ALLOW_ORIGIN string = "$allowOrigin"

var DEFAULT_LOG_PORT string = "30081"
var DEFAULT_EDITOR_PORT string = "30082"
var DEFAULT_PROXY_PORT string = "30083"

type GetPersistentVolumeResponse struct {
	Kind string `json:"kind"`
}

type SavePersistentVolumeResponse struct {
	Kind string `json:"kind"`
}

type GetPersistentVolumeClaimResponse struct {
	Kind string `json:"kind"`
}

type SavePersistentVolumeClaimResponse struct {
	Kind string `json:"kind"`
}

type GetDeploymentResponse struct {
	Kind string `json:"kind"`
}

type SaveDeploymentResponse struct {
	Kind string `json:"kind"`
}

type DeleteDeploymentResponse struct {
	Kind string `json:"kind"`
}

type GetReplicaSetsResponse struct {
	Kind string `json:"kind"`
	Items []*GetReplicaSetsItems `json:"items"`
}

type GetReplicaSetsItems struct {
	Metadata *GetReplicaSetsItemMetadata `json:"metadata"`
}

type GetReplicaSetsItemMetadata struct {
	Name string `json:"name"`
	Labels *GetReplicaSetsItemMetadataLabel `json:"labels"`
}

type GetReplicaSetsItemMetadataLabel struct {
	App string `josn:"app"`
}

type DeleteReplicaSetBody struct {
	Kind string `json:"kind"`
	OrphanDependents bool `json:"orphanDependents"`
}

type DeleteReplicaSetResponse struct {
	Kind string `json:"kind"`
}

type GetPodsResponse struct {
	Kind string `json:"kind"`
	Items []*GetPodsItems `json:"items"`
}

type GetPodsItems struct {
	Metadata *GetPodsItemMetadata `json:"metadata"`
}

type GetPodsItemMetadata struct {
	Name string `json:"name"`
	Labels *GetPodsItemMetadataLabel `json:"labels"`
}

type GetPodsItemMetadataLabel struct {
	App string `josn:"app"`
}

type DeletePodResponse struct {
	Kind string `json:"kind"`
}

type GetServiceResponse struct {
	Kind string `json:"kind"`
}

type SaveServiceResponse struct {
	Kind string `json:"kind"`
	Spec *ServiceSpec `json:"spec"`
}

type ServiceSpec struct {
	Ports []*ServiceSpecPort `json:"ports"`
}

type ServiceSpecPort struct {
	Name string `json:"name"`
	NodePort int `json:"nodePort"`
}

type DeleteServiceResponse struct {
	Kind string `json:"kind"`
}

type MinienvConfig struct {
	Editor *MinienvConfigEditor `json:"editor"`
	Proxy *MinienvConfigProxy `json:"proxy"`
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

func getHttpClient() *http.Client {
	// mw:FIX THIS
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	return client
}

func getPersistentVolume(name string, kubeServiceToken string, kubeServiceBaseUrl string) (*GetPersistentVolumeResponse, error) {
	url := fmt.Sprintf("%s/api/v1/persistentvolumes/%s", kubeServiceBaseUrl, name)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting service: ", err)
		return nil, err
	} else {
		var getPersistentVolumeResp GetPersistentVolumeResponse
		err := json.NewDecoder(resp.Body).Decode(&getPersistentVolumeResp)
		if err != nil {
			return nil, err
		} else if getPersistentVolumeResp.Kind != "PersistentVolume" {
			return nil, nil
		} else {
			return &getPersistentVolumeResp, nil
		}
	}
}

func savePersistentVolume(yaml string, kubeServiceToken string, kubeServiceBaseUrl string) (*SavePersistentVolumeResponse, error) {
	url := fmt.Sprintf("%s/api/v1/persistentvolumes", kubeServiceBaseUrl)
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, strings.NewReader(yaml))
	req.Header.Add("Content-Type", "application/yaml")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Print("Error saving persistent volume: ", err)
		return nil, err
	} else {
		var savePersistentVolumeResp SavePersistentVolumeResponse
		err := json.NewDecoder(resp.Body).Decode(&savePersistentVolumeResp)
		if err != nil {
			return nil, err
		} else if savePersistentVolumeResp.Kind != "PersistentVolume" {
			return nil, errors.New("Unable to create persistent volume")
		} else {
			return &savePersistentVolumeResp, nil
		}
	}
}

func getPersistentVolumeClaim(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetPersistentVolumeClaimResponse, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/persistentvolumeclaims/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting service: ", err)
		return nil, err
	} else {
		var getPersistentVolumeClaimResp GetPersistentVolumeClaimResponse
		err := json.NewDecoder(resp.Body).Decode(&getPersistentVolumeClaimResp)
		if err != nil {
			return nil, err
		} else if getPersistentVolumeClaimResp.Kind != "PersistentVolumeClaim" {
			return nil, nil
		} else {
			return &getPersistentVolumeClaimResp, nil
		}
	}
}

func savePersistentVolumeClaim(yaml string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*SavePersistentVolumeClaimResponse, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/persistentvolumeclaims", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, strings.NewReader(yaml))
	req.Header.Add("Content-Type", "application/yaml")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Print("Error saving persistent volume claim: ", err)
		return nil, err
	} else {
		var savePersistentVolumeClaimResp SavePersistentVolumeClaimResponse
		err := json.NewDecoder(resp.Body).Decode(&savePersistentVolumeClaimResp)
		if err != nil {
			return nil, err
		} else if savePersistentVolumeClaimResp.Kind != "PersistentVolumeClaim" {
			return nil, errors.New("Unable to create persistent volume claim")
		} else {
			return &savePersistentVolumeClaimResp, nil
		}
	}
}

func getDeployment(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetDeploymentResponse, error) {
	log.Println("getDeployment.kubeServiceToken:")
	log.Println(kubeServiceToken)
	log.Println("getDeployment.kubeServiceBaseUrl:")
	log.Println(kubeServiceBaseUrl)
	url := fmt.Sprintf("%s/apis/extensions/v1beta1/namespaces/%s/deployments/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting deployment: ", err)
		return nil, err
	} else {
		var getDeploymentResp GetDeploymentResponse
		err := json.NewDecoder(resp.Body).Decode(&getDeploymentResp)
		if err != nil {
			return nil, err
		} else if getDeploymentResp.Kind != "Deployment" {
			return nil, nil
		} else {
			return &getDeploymentResp, nil
		}
	}
}

func saveDeployment(yaml string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*SaveDeploymentResponse, error) {
	url := fmt.Sprintf("%s/apis/extensions/v1beta1/namespaces/%s/deployments", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, strings.NewReader(yaml))
	req.Header.Add("Content-Type", "application/yaml")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error saving deployment: ", err)
		return nil, err
	} else {
		var saveDeploymentResp SaveDeploymentResponse
		err := json.NewDecoder(resp.Body).Decode(&saveDeploymentResp)
		if err != nil {
			return nil, err
		} else if saveDeploymentResp.Kind != "Deployment" {
			return nil, errors.New("Unable to create deployment")
		} else {
			return &saveDeploymentResp, nil
		}
	}
}

func deleteDeployment(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Deleting deployment '%s'...\n", name)
	url := fmt.Sprintf("%s/apis/extensions/v1beta1/namespaces/%s/deployments/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting deployment: ", err)
		return false, err
	} else {
		var deleteDeploymentResp DeleteDeploymentResponse
		err := json.NewDecoder(resp.Body).Decode(&deleteDeploymentResp)
		if err != nil {
			return false, err
		} else {
			return deleteDeploymentResp.Kind == "Deployment", nil
		}
	}
}

func getReplicaSets(kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetReplicaSetsResponse, error) {
	url := fmt.Sprintf("%s/apis/extensions/v1beta1/namespaces/%s/replicasets", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting replica sets: ", err)
		return nil, err
	} else {
		var getReplicaSetsResponse GetReplicaSetsResponse
		err := json.NewDecoder(resp.Body).Decode(&getReplicaSetsResponse)
		if err != nil {
			return nil, err
		} else if getReplicaSetsResponse.Kind != "ReplicaSetList" {
			return nil, nil
		} else {
			return &getReplicaSetsResponse, nil
		}
	}
}

func getReplicaSetName(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (string, error) {
	log.Printf("Getting replica set name for label '%s'...\n", label)
	getReplicaSetsResponse, err := getReplicaSets(kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		return "", err
	} else {
		if getReplicaSetsResponse.Items != nil && len(getReplicaSetsResponse.Items) > 0 {
			for _, element := range getReplicaSetsResponse.Items {
				if element.Metadata != nil && element.Metadata.Labels != nil && element.Metadata.Labels.App == label {
					log.Printf("Replica set name for label '%s' = '%s'\n", label, element.Metadata.Name)
					return element.Metadata.Name, nil
				}
			}
		}
		return "", nil
	}
}

func deleteReplicaSet(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Deleting replica set for label '%s'...\n", label)
	name, err := getReplicaSetName(label, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error deleting replica set: ", err)
		return false, err
	} else if name == "" {
		return false, nil
	}
	// delete replica set
	log.Printf("Deleting replica set '%s'...\n", name)
	url := fmt.Sprintf("%s/apis/extensions/v1beta1/namespaces/%s/replicasets/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	body := &DeleteReplicaSetBody{Kind: "DeleteOptions", OrphanDependents: false}
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(body)
	req, err := http.NewRequest("DELETE", url, b)
	req.Header.Add("Content-Type", "application/json")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting replica set: ", err)
		return false, err
	} else {
		var deleteReplicaSetResp DeleteReplicaSetResponse
		err := json.NewDecoder(resp.Body).Decode(&deleteReplicaSetResp)
		if err != nil {
			return false, err
		} else {
			return deleteReplicaSetResp.Kind == "ReplicaSet", nil
		}
	}
}

func getPods(kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetPodsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/pods", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting pods: ", err)
		return nil, err
	} else {
		var getPodsResponse GetPodsResponse
		err := json.NewDecoder(resp.Body).Decode(&getPodsResponse)
		if err != nil {
			return nil, err
		} else if getPodsResponse.Kind != "PodList" {
			return nil, nil
		} else {
			return &getPodsResponse, nil
		}
	}
}

func getPodName(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (string, error) {
	log.Printf("Getting pod name for label '%s'...\n", label)
	getPodsResponse, err := getPods(kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		return "", err
	} else {
		if getPodsResponse.Items != nil && len(getPodsResponse.Items) > 0 {
			for _, element := range getPodsResponse.Items {
				if element.Metadata != nil && element.Metadata.Labels != nil && element.Metadata.Labels.App == label {
					log.Printf("Pod name for label '%s' = '%s'\n", label, element.Metadata.Name)
					return element.Metadata.Name, nil
				}
			}
		}
		return "", nil
	}
}

func deletePod(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Deleting pod for label '%s'...\n", label)
	name, err := getPodName(label, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error deleting pod: ", err)
		return false, err
	} else if name == "" {
		return false, nil
	}
	// delete pod
	log.Printf("Deleting pod '%s'...\n", name)
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting pod: ", err)
		return false, err
	} else {
		var deletePodResp DeletePodResponse
		err := json.NewDecoder(resp.Body).Decode(&deletePodResp)
		if err != nil {
			return false, err
		} else {
			return deletePodResp.Kind == "Pod", nil
		}
	}
}

func waitForPodTermination(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Waiting for pod termination for label '%s'...\n", label)
	i := 0
	for i < 6 {
		i++
		time.Sleep(5 *time.Second)
		name, err := getPodName(label, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error waiting for pod termination: ", err)
			return false, err
		} else if name == "" {
			return true, nil
		}
	}
	return false, nil
}

func getService(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetServiceResponse, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting service: ", err)
		return nil, err
	} else {
		var getServiceResp GetServiceResponse
		err := json.NewDecoder(resp.Body).Decode(&getServiceResp)
		if err != nil {
			return nil, err
		} else if getServiceResp.Kind != "Service" {
			return nil, nil
		} else {
			return &getServiceResp, nil
		}
	}
}

func saveService(yaml string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*SaveServiceResponse, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/services", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, strings.NewReader(yaml))
	req.Header.Add("Content-Type", "application/yaml")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Print("Error saving service: ", err)
		return nil, err
	} else {
		var saveServiceResp SaveServiceResponse
		err := json.NewDecoder(resp.Body).Decode(&saveServiceResp)
		if err != nil {
			return nil, err
		} else if saveServiceResp.Kind != "Service" {
			return nil, errors.New("Unable to create service: " + saveServiceResp.Kind)
		} else {
			return &saveServiceResp, nil
		}
	}
}

func deleteService(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Deleting service '%s'...\n", name)
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting service: ", err)
		return false, err
	} else {
		var deleteServiceResp DeleteServiceResponse
		err := json.NewDecoder(resp.Body).Decode(&deleteServiceResp)
		if err != nil {
			return false, err
		} else {
			return deleteServiceResp.Kind == "Service", nil
		}
	}
}

func isExampleDeployed(userId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	getDeploymentResp, err := getDeployment(getDeploymentName(userId), kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		return false, err
	} else {
		return getDeploymentResp != nil, nil
	}
}

func deleteExample(userId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) {
	log.Printf("Deleting example for user '%s'...\n", userId)
	deploymentName := getDeploymentName(userId)
	appLabel := getAppLabel(userId)
	serviceName := getServiceName(userId)
	_, _ = deleteDeployment(deploymentName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteReplicaSet(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	//_, _ = deletePod(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = deleteService(serviceName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	_, _ = waitForPodTermination(appLabel, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
}

func deployExample(userId string, gitRepo string, storageDriver string, pvTemplate string, pvcTemplate string, deploymentTemplate string, serviceTemplate string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*DeploymentDetails, error) {
	// delete example, if it exists
	deleteExample(userId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	// download minienv.json
	var minienvConfig MinienvConfig
	minienvConfigUrl := fmt.Sprintf("%s/raw/master/minienv.json", gitRepo)
	log.Printf("Downloading minienv config from '%s'...\n", minienvConfigUrl)
	client := getHttpClient()
	req, err := http.NewRequest("GET", minienvConfigUrl, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error downloading minienv config: ", err)
	} else {
		err = json.NewDecoder(resp.Body).Decode(&minienvConfig)
		if err != nil {
			log.Println("Error downloading minienv config: ", err)
		} else {

		}
	}
	// download docker-compose file (first try yml, then yaml)
	tabs := []*Tab{}
	dockerComposeUrl := fmt.Sprintf("%s/raw/master/docker-compose.yml", gitRepo)
	log.Printf("Downloading docker-compose file from '%s'...\n", dockerComposeUrl)
	client = getHttpClient()
	req, err = http.NewRequest("GET", dockerComposeUrl, nil)
	resp, err = client.Do(req)
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
	// create persistent volume if not exits
	pvName := getPersistentVolumeName(userId)
	pvPath := getPersistentVolumePath(userId)
	pvResponse, err := getPersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
	if err != nil {
		log.Println("Error saving persistent volume: ", err)
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
	// create persistent volume claim, if not exists
	pvcName := getPersistentVolumeClaimName(userId)
	pvcResponse, err := getPersistentVolumeClaim(pvcName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving persistent volume claim: ", err)
		return nil, err
	} else if pvcResponse == nil {
		pvc := pvcTemplate
		pvc = strings.Replace(pvc, VAR_PVC_NAME, pvcName, -1)
		_, err = savePersistentVolumeClaim(pvc, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error saving persistent volume claim: ", err)
			return nil, err
		}
	}
	// create deployment
	appLabel := getAppLabel(userId)
	deploymentName := getDeploymentName(userId)
	deployment := deploymentTemplate
	deployment = strings.Replace(deployment, VAR_DEPLOYMENT_NAME, deploymentName, -1)
	deployment = strings.Replace(deployment, VAR_APP_LABEL, appLabel, -1)
	deployment = strings.Replace(deployment, VAR_STORAGE_DRIVER, storageDriver, -1)
	deployment = strings.Replace(deployment, VAR_LOG_PORT, DEFAULT_LOG_PORT, -1)
	deployment = strings.Replace(deployment, VAR_EDITOR_PORT, DEFAULT_EDITOR_PORT, -1)
	deployment = strings.Replace(deployment, VAR_PROXY_PORT, DEFAULT_PROXY_PORT, -1)
	deployment = strings.Replace(deployment, VAR_GIT_REPO, gitRepo, -1)
	deployment = strings.Replace(deployment, VAR_ALLOW_ORIGIN, allowOrigin, -1)
	deployment = strings.Replace(deployment, VAR_PVC_NAME, pvcName, -1)
	_, err = saveDeployment(deployment, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving deployment: ", err)
		return nil, err
	}
	// deployment created, now create the service
	serviceName := getServiceName(userId)
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
	} else {
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
		details.NodeHostName = os.Getenv("EXUP_NODE_HOST_NAME") // mw:TODO
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
		return details, nil
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

func getPersistentVolumeName(userId string) string {
	return strings.ToLower(fmt.Sprintf("u-%s-pv", userId))
}

func getPersistentVolumePath(userId string) string {
	return strings.ToLower(fmt.Sprintf("/u-%s-docker", userId))
}

func getPersistentVolumeClaimName(userId string) string {
	return strings.ToLower(fmt.Sprintf("u-%s-pvc", userId))
}

func getServiceName(userId string) string {
	return strings.ToLower(fmt.Sprintf("u-%s-service", userId))
}

func getDeploymentName(userId string) string {
	return strings.ToLower(fmt.Sprintf("u-%s-deployment", userId))
}

func getAppLabel(userId string) string {
	return strings.ToLower(fmt.Sprintf("u-%s", userId))
}