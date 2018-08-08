package main

import (
	"net/http"
	"encoding/json"
	"log"
	"bytes"
	"errors"
	"strings"
	"time"
)

const CacheRepoAllowedSeconds int64 = 20 * 60
const CacheRepoDeniedSeconds int64 = 5 * 60

var allUserReposAllowed = make(map[string]map[string]int64)
var allUserReposDenied = make(map[string]map[string]int64)

type GitHubAuthProvider struct {
	ClientId string
	ClientSecret string
}

type GitHubAuthTokenRequest struct {
	ClientId string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code string `json:"code"`
}

type GitHubAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type GitHubRepoResponse struct {
	Private bool `json:"private"`
	Permissions *GitHubRepoResponsePermissions `json:"permissions"`
}

type GitHubRepoResponsePermissions struct {
	Admin bool `json:"id"`
	Push bool `json:"push"`
	Pull bool `json:"pull"`
}

func (authProvider GitHubAuthProvider) onAuthCallback(parameters map[string][]string) (*User, error) {
	codeVals, ok := parameters["code"]
	if !ok || len(codeVals[0]) < 1 {
		log.Println("GitHub Auth: Missing code parameter")
		return nil, errors.New("missing code parameter")
	}
	url := "https://github.com/login/oauth/access_token"
	authTokenRequest := GitHubAuthTokenRequest{
		ClientId: authProvider.ClientId,
		ClientSecret: authProvider.ClientSecret,
		Code: codeVals[0],
	}
	b, err := json.Marshal(authTokenRequest)
	if err != nil {
		log.Println("GitHub Auth: Error serializing GitHub auth token request")
		return nil, err
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
		log.Println("GitHub Auth: Error getting access token: ", err)
		return nil, err
	}
	var authTokenResponse GitHubAuthTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&authTokenResponse)
	if err != nil {
		log.Println("GitHub Auth: Error getting access token: ", err)
		return nil, err
	}
	return &User{
		AccessToken: authTokenResponse.AccessToken,
		Email: authTokenResponse.AccessToken,
	}, nil
}

func (authProvider GitHubAuthProvider) loginUser(accessToken string) (*User, error) {
	client := getHttpClient()
	url := "https://api.github.com/user?access_token=" + accessToken
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	_, err = client.Do(req)
	if err != nil {
		log.Println("GitHub Auth: Invalid access token: ", err)
		return nil, err
	}
	return &User{
		AccessToken: accessToken,
		Email: accessToken,
	}, nil
}

func (authProvider GitHubAuthProvider) userCanViewRepo(user *User, repo string) (bool, error) {
	now := int64(time.Now().Unix())
	userReposAllowed := allUserReposAllowed[user.AccessToken]
	if userReposAllowed == nil {
		userReposAllowed = make(map[string]int64)
		allUserReposAllowed[user.AccessToken] = userReposAllowed
	}
	userReposDenied := allUserReposDenied[user.AccessToken]
	if userReposDenied == nil {
		userReposDenied = make(map[string]int64)
		allUserReposDenied[user.AccessToken] = userReposDenied
	}
	if expires, ok := userReposAllowed[repo]; ok {
		if expires > now {
			return true, nil
		}
	}
	if expires, ok := userReposDenied[repo]; ok {
		if expires > now {
			return false, nil
		}
	}
	client := getHttpClient()
	url := strings.Replace(repo, "github.com", "api.github.com/repos", 1)
	url += "?access_token=" + user.AccessToken
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Println("GitHub Auth: Error getting repo: ", err)
		return false, err
	}
	var repoResponse GitHubRepoResponse
	err = json.NewDecoder(resp.Body).Decode(&repoResponse)
	if err != nil {
		log.Println("GitHub Auth: Error getting repo: ", err)
		return false, err
	}
	canView := false
	if repoResponse.Permissions != nil {
		if repoResponse.Private {
			canView = repoResponse.Permissions.Admin || repoResponse.Permissions.Push
		} else {
			canView = repoResponse.Permissions.Pull
		}
	}
	if canView {
		userReposAllowed[repo] = now + CacheRepoAllowedSeconds
	} else {
		userReposDenied[repo] = now + CacheRepoDeniedSeconds
	}
	return canView, nil
}