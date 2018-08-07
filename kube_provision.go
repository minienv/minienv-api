package main

import (
	"fmt"
	"log"
	"strings"
)

var VarJobName string = "$jobName"
var VarProvisionImages string = "$provisionImages"

var PodPhaseSuccess = "Succeeded"
var PodPhaseFailure = "Failed"

func isProvisionerRunning(envId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	label := getProvisionerAppLabel(envId)
	log.Printf("Getting pod name for label '%s'...\n", label)
	getPodsResponse, err := getPods(kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error getting pods.", err)
		return false, err
	} else {
		if getPodsResponse.Items != nil && len(getPodsResponse.Items) > 0 {
			for _, element := range getPodsResponse.Items {
				if element.Metadata != nil && element.Metadata.Labels != nil && element.Metadata.Labels.App == label {
					log.Printf("Pod found for label '%s'.\n", label)
					if element.Status != nil && element.Status.Phase != "" {
						log.Printf("Status for pod '%s' = '%s'.\n", label, element.Status.Phase)
						if element.Status.Phase != PodPhaseSuccess && element.Status.Phase != PodPhaseFailure {
							return true, nil
						}
					} else {
						return true, nil
					}
				}
			}
		}
		return false, nil
	}
}

func deleteProvisioner(envId string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (bool, error) {
	deleted, err := deleteJob(getProvisionerJobName(envId), kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		return false, err
	}
	// delete all pods
	label := getProvisionerAppLabel(envId)
	getPodsResponse, err := getPods(kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error getting pods for delete job.", err)
	} else {
		if getPodsResponse.Items != nil && len(getPodsResponse.Items) > 0 {
			for _, element := range getPodsResponse.Items {
				if element.Metadata != nil && element.Metadata.Labels != nil && element.Metadata.Labels.App == label {
					deletePod(element.Metadata.Name, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
				}
			}
		}
		return false, nil
	}
	return deleted, err
}

func deployProvisioner(minienvVersion string, envId string, nodeNameOverride string, storageDriver string, pvTemplate string, pvcTemplate string, jobTemplate string, provisionVolumeSize string, provisionImages string, kubeServiceToken string, kubeServiceBaseUrl string, kubeNamespace string) (error) {
	// delete example, if it exists
	deleteProvisioner(envId, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	// create persistent volume if using host paths
	if envPvHostPath {
		pvName := getPersistentVolumeName(envId)
		pvPath := getPersistentVolumePath(envId)
		pvResponse, err := getPersistentVolume(pvName, kubeServiceToken, kubeServiceBaseUrl)
		if err != nil {
			log.Println("Error getting persistent volume: ", err)
			return err
		} else if pvResponse == nil {
			pv := pvTemplate
			pv = strings.Replace(pv, VarPvName, pvName, -1)
			pv = strings.Replace(pv, VarPvSize, provisionVolumeSize, -1)
			pv = strings.Replace(pv, VarPvPath, pvPath, -1)
			_, err = savePersistentVolume(pv, kubeServiceToken, kubeServiceBaseUrl)
			if err != nil {
				log.Println("Error saving persistent volume: ", err)
				return err
			}
		}
	}
	// create persistent volume claim, if not exists
	pvcName := getPersistentVolumeClaimName(envId)
	pvcResponse, err := getPersistentVolumeClaim(pvcName, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error getting persistent volume claim: ", err)
		return err
	} else if pvcResponse == nil {
		pvc := pvcTemplate
		pvc = strings.Replace(pvc, VarPvSize, provisionVolumeSize, -1)
		pvc = strings.Replace(pvc, VarPvcName, pvcName, -1)
		pvc = strings.Replace(pvc, VarPvcStorageClass, envPvcStorageClass, -1)
		_, err = savePersistentVolumeClaim(pvc, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
		if err != nil {
			log.Println("Error saving persistent volume claim: ", err)
			return err
		}
	}
	// create job
	jobName := getProvisionerJobName(envId)
	appLabel := getProvisionerAppLabel(envId)
	job := jobTemplate
	job = strings.Replace(job, VarMinienvNodeNameOverride, nodeNameOverride, -1)
	job = strings.Replace(job, VarMinienvVersion, minienvVersion, -1)
	job = strings.Replace(job, VarJobName, jobName, -1)
	job = strings.Replace(job, VarAppLabel, appLabel, -1)
	job = strings.Replace(job, VarStorageDriver, storageDriver, -1)
	job = strings.Replace(job, VarProvisionImages, provisionImages, -1)
	job = strings.Replace(job, VarPvcName, pvcName, -1)
	_, err = saveJob(job, kubeServiceToken, kubeServiceBaseUrl, kubeNamespace)
	if err != nil {
		log.Println("Error saving job: ", err)
		return err
	}
	return nil
}

func getProvisionerJobName(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-provision-job", envId))
}

func getProvisionerAppLabel(envId string) string {
	return strings.ToLower(fmt.Sprintf("env-%s-provision", envId))
}