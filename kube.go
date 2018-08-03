package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type StatusResponse struct {
	Kind string `json:"kind"`
	Status string `json:"status"`
}

type GetPersistentVolumeResponse struct {
	Kind string `json:"kind"`
}

type SavePersistentVolumeResponse struct {
	Kind string `json:"kind"`
}

type DeletePersistentVolumeResponse struct {
	StatusResponse
}

type GetPersistentVolumeClaimResponse struct {
	Kind string `json:"kind"`
}

type SavePersistentVolumeClaimResponse struct {
	Kind string `json:"kind"`
}

type DeletePersistentVolumeClaimResponse struct {
	StatusResponse
}

type GetJobResponse struct {
	Kind string `json:"kind"`
}

type SaveJobResponse struct {
	Kind string `json:"kind"`
}

type DeleteJobResponse struct {
	StatusResponse
}

type GetDeploymentResponse struct {
	Kind string `json:"kind"`
	Spec *GetDeploymentResponseSpec `json:"spec"`
}

type GetDeploymentResponseSpec struct {
	Template *GetDeploymentResponseSpecTemplate `json:"template"`
}

type GetDeploymentResponseSpecTemplate struct {
	Metadata *GetDeploymentSpecTemplateMetadata `json:"metadata"`
}

type GetDeploymentSpecTemplateMetadata struct {
	Annotations *GetDeploymentSpecTemplateMetadataAnnotation `json:"annotations"`
}

type GetDeploymentSpecTemplateMetadataAnnotation struct {
	Repo string `json:"minienv.repo"`
	RepoWithCreds string  `json:"minienv.repoWithCreds"`
	Branch string `json:"minienv.branch"`
	ClaimToken string `json:"minienv.claimToken"`
	EnvDetails string `json:"minienv.envDetails"`
}

type SaveDeploymentResponse struct {
	Kind string `json:"kind"`
}

type DeleteDeploymentResponse struct {
	StatusResponse
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
	App string `json:"app"`
}

type DeleteReplicaSetBody struct {
	Kind string `json:"kind"`
	OrphanDependents bool `json:"orphanDependents"`
}

type DeleteReplicaSetResponse struct {
	StatusResponse
}

type GetPodsResponse struct {
	Kind string `json:"kind"`
	Items []*GetPodsItems `json:"items"`
}

type GetPodsItems struct {
	Metadata *GetPodsItemMetadata `json:"metadata"`
	Status *GetPodsItemStatus `json:"status"`
}

type GetPodsItemStatus struct {
	Phase string `json:"phase"`
}

type GetPodsItemMetadata struct {
	Name string `json:"name"`
	Labels *GetPodsItemMetadataLabel `json:"labels"`
}

type GetPodsItemMetadataLabel struct {
	App string `json:"app"`
}

type DeletePodResponse struct {
	StatusResponse
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
	StatusResponse
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
		log.Println("Error getting persistent volume: ", err)
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

func deletePersistentVolume(name string, kubeServiceToken string, kubeServiceBaseUrl string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/persistentvolumes/%s", kubeServiceBaseUrl, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting persistent volume: ", err)
		return false, err
	} else {
		var deletePersistentVolumeResp DeletePersistentVolumeResponse
		err := json.NewDecoder(resp.Body).Decode(&deletePersistentVolumeResp)
		if err != nil {
			return false, err
		} else {
			return deletePersistentVolumeResp.Status == "Success", nil
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

func deletePersistentVolumeClaim(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/persistentvolumeclaims/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting persistent volume claim: ", err)
		return false, err
	} else {
		var deletePersistentVolumeClaimResp DeletePersistentVolumeClaimResponse
		err := json.NewDecoder(resp.Body).Decode(&deletePersistentVolumeClaimResp)
		if err != nil {
			return false, err
		} else {
			return deletePersistentVolumeClaimResp.Status == "Success", nil
		}
	}
}

func getJob(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetJobResponse, error) {
	url := fmt.Sprintf("%s/apis/batch/v1/namespaces/%s/jobs/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("GET", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error getting job: ", err)
		return nil, err
	} else {
		var getJobResp GetJobResponse
		err := json.NewDecoder(resp.Body).Decode(&getJobResp)
		if err != nil {
			return nil, err
		} else if getJobResp.Kind != "Job" {
			return nil, nil
		} else {
			return &getJobResp, nil
		}
	}
}

func saveJob(yaml string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*SaveJobResponse, error) {
	url := fmt.Sprintf("%s/apis/batch/v1/namespaces/%s/jobs", kubeServiceBaseUrl, kubeNamespace)
	client := getHttpClient()
	req, err := http.NewRequest("POST", url, strings.NewReader(yaml))
	req.Header.Add("Content-Type", "application/yaml")
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error saving job: ", err)
		return nil, err
	} else {
		var saveJobResp SaveJobResponse
		err := json.NewDecoder(resp.Body).Decode(&saveJobResp)
		if err != nil {
			return nil, err
		} else if saveJobResp.Kind != "Job" {
			return nil, errors.New("Unable to create job")
		} else {
			return &saveJobResp, nil
		}
	}
}

func deleteJob(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Deleting job '%s'...\n", name)
	url := fmt.Sprintf("%s/apis/batch/v1/namespaces/%s/jobs/%s", kubeServiceBaseUrl, kubeNamespace, name)
	client := getHttpClient()
	req, err := http.NewRequest("DELETE", url, nil)
	if len(kubeServiceToken) > 0 {
		req.Header.Add("Authorization", "Bearer " + kubeServiceToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error deleting job: ", err)
		return false, err
	} else {
		var deleteJobResp DeleteJobResponse
		err := json.NewDecoder(resp.Body).Decode(&deleteJobResp)
		if err != nil {
			return false, err
		} else {
			return deleteJobResp.Status == "Success", nil
		}
	}
}

func getDeployment(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (*GetDeploymentResponse, error) {
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
			return deleteDeploymentResp.Status == "Success", nil
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
			return deleteReplicaSetResp.Status == "Success", nil
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

func deletePod(name string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
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
			return deletePodResp.Status == "Success", nil
		}
	}
}

func waitForPodTermination(label string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	log.Printf("Waiting for pod termination for label '%s'...\n", label)
	i := 0
	for i < 6 {
		i++
		name, err := getPodName(label, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error waiting for pod termination: ", err)
			return false, err
		} else if name == "" {
			return true, nil
		}
		time.Sleep(5 *time.Second)
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
			return deleteServiceResp.Status == "Success", nil
		}
	}
}