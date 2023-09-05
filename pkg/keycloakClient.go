package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

type KeycloakClient struct {
	baseURL                url.URL
	username               string
	password               string
	clientId               string
	realm                  string
	adminGroupPath         string
	autoAssignOrgGroupPath string
	country                string
	adminGroup             *KeycloakGroup
	client                 *http.Client
}

type KeycloakUser struct {
	Id        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Username  string `json:"username"`
}

type KeycloakGroup struct {
	Id           string               `json:"id"`
	Name         string               `json:"name"`
	Path         string               `json:"path"`
	SubGroups    []*KeycloakGroup     `json:"subGroups"`
	Attributes   *map[string][]string `json:"attributes"`
	pathElements []string             `json:"-"` // transient
}

func (this *KeycloakGroup) GetDisplayNameAttribute() string {
	if this.Attributes != nil {
		displayNames, ok := (*this.Attributes)["displayName"]
		if ok && len(displayNames) > 0 {
			return displayNames[0]
		}
	}
	return ""
}

func (this *KeycloakGroup) GetPathElements() []string {
	if this.pathElements == nil {
		path := this.Path
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		this.pathElements = strings.Split(path, "/")
	}
	return this.pathElements
}

func (this *KeycloakGroup) IsSameOrganization(other *KeycloakGroup) bool {
	if other == nil {
		return false
	}
	return this.GetPathElements()[0] == other.GetPathElements()[0] && this.GetPathElements()[1] == other.GetPathElements()[1]
}

func (this *KeycloakGroup) GetOrganizationName() string {
	if len(this.GetPathElements()) < 2 {
		return ""
	}
	return this.GetPathElements()[1]
}

func (this *KeycloakUser) GetDisplayName() string {
	if this.FirstName == "" && this.LastName == "" {
		return this.Email
	}
	if this.LastName == "" {
		return this.FirstName
	}
	if this.FirstName == "" {
		return this.LastName
	}
	return this.FirstName + " " + this.LastName
}

func NewKeycloakClient(baseURL string, realm string, username string, password string, clientId string, adminGroupPath string, autoAssignOrgGroupPath string) (*KeycloakClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{} // Creating the transport explicitly allows for connection pooling and reuse
	cli := &http.Client{Transport: tr}
	if err != nil {
		return nil, err
	}

	return &KeycloakClient{
		baseURL:                *u,
		client:                 cli,
		realm:                  realm,
		username:               username,
		password:               password,
		clientId:               clientId,
		adminGroupPath:         adminGroupPath,
		autoAssignOrgGroupPath: autoAssignOrgGroupPath,
	}, nil
}

func (this *KeycloakClient) GetToken() (string, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/auth/realms/%s/protocol/openid-connect/token", this.baseURL.String(), this.realm), nil)
	if err != nil {
		return "", err
	}

	req.Header["Accept"] = []string{"application/json"}
	req.Header["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	req.Header["cache-control"] = []string{"no-cache"}

	data := fmt.Sprintf("grant_type=password&username=%s&password=%s&client_id=%s", url.QueryEscape(this.username), url.QueryEscape(this.password), url.QueryEscape(this.clientId))
	req.Body = io.NopCloser(strings.NewReader(data))

	r, err := this.client.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}

	var objmap map[string]interface{}
	json.Unmarshal(body, &objmap)

	accessToken, ok := objmap["access_token"]
	if !ok {
		return "", errors.New("access_token not found in JSON response")
	}
	return fmt.Sprintf("%s", accessToken), nil
}

func (this *KeycloakClient) GetUsers(token string) ([]*KeycloakUser, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/auth/admin/realms/%s/users?max=100000", this.baseURL.String(), this.realm), nil)
	if err != nil {
		return nil, err
	}

	req.Header["Authorization"] = []string{"Bearer " + token}
	req.Header["cache-control"] = []string{"no-cache"}

	r, err := this.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	keycloakUsers := make([]*KeycloakUser, 0)
	err = json.Unmarshal(body, &keycloakUsers)
	if err != nil {
		return nil, err
	}
	return keycloakUsers, nil
}

func (this *KeycloakClient) GetGroups(token string) ([]*KeycloakGroup, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/auth/admin/realms/%s/groups?max=100000&briefRepresentation=false", this.baseURL.String(), this.realm), nil)
	if err != nil {
		return nil, err
	}

	req.Header["Authorization"] = []string{"Bearer " + token}
	req.Header["cache-control"] = []string{"no-cache"}

	r, err := this.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	keycloakGroups := make([]*KeycloakGroup, 0)
	err = json.Unmarshal(body, &keycloakGroups)
	if err != nil {
		return nil, err
	}
	return keycloakGroups, nil
}

// This returns all Keycloak groups with two-level path "/organizations/[ORGNAME]", but not "/organizations/[ORGNAME]/[TEAMNAME]"
// The returned groups may have subgroups (teams), but the subgroups themselves are not part of the list.
func (this *KeycloakClient) GetOrganizations(token string) ([]*KeycloakGroup, error) {
	allGroups, err := this.GetGroups(token)
	if err != nil {
		return nil, err
	}

	for _, group := range allGroups {
		if group.Path == "/organizations" {
			return group.SubGroups, nil
		}
	}

	return []*KeycloakGroup{}, nil
}

func (this *KeycloakClient) findSubgroup(groups []*KeycloakGroup) {
	for _, group := range groups {
		if group.Path == this.adminGroupPath {
			this.adminGroup = group
			break
		}
		this.findSubgroup(group.SubGroups)
		if this.adminGroup != nil {
			break
		}
	}
}

func (this *KeycloakClient) GetGroupMembership(token string, user *KeycloakUser) ([]*KeycloakGroup, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/auth/admin/realms/%s/users/%s/groups", this.baseURL.String(), this.realm, user.Id), nil)
	if err != nil {
		return nil, err
	}

	req.Header["Authorization"] = []string{"Bearer " + token}
	req.Header["cache-control"] = []string{"no-cache"}

	response, err := this.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	groups := make([]*KeycloakGroup, 0)
	err = json.Unmarshal(body, &groups)
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func (this *KeycloakClient) worker(token string, userChan chan *KeycloakUser, results *sync.Map, errorCount *uint64, wg *sync.WaitGroup) {
	defer wg.Done()

	for user := range userChan {
		groups, err := this.GetGroupMembership(token, user)
		if err != nil {
			atomic.AddUint64(errorCount, 1)
			klog.Error(err)
		}
		results.Store(user, groups)
	}
}

func (this *KeycloakClient) GetGroupMemberships(token string, users []*KeycloakUser) (map[*KeycloakUser][]*KeycloakGroup, error) {
	results := sync.Map{}
	var errorCount uint64

	userChan := make(chan *KeycloakUser)
	wg := new(sync.WaitGroup)

	// creating workers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go this.worker(token, userChan, &results, &errorCount, wg)
	}

	// sending users to workers
	for _, user := range users {
		userChan <- user
	}

	close(userChan)
	wg.Wait()

	if errorCount > 0 {
		return nil, errors.New("Could not fetch all group memberships")
	}

	userGroups := make(map[*KeycloakUser][]*KeycloakGroup)
	results.Range(func(k, v interface{}) bool {
		userGroups[k.(*KeycloakUser)] = v.([]*KeycloakGroup)
		return true
	})

	return userGroups, nil
}

func (this *KeycloakClient) CloseIdleConnections() {
	this.client.CloseIdleConnections()
}
