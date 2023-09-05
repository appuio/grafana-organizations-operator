package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	grafana "github.com/grafana/grafana-api-golang-client"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type UserOrg struct {
	OrgID int64  `json:"orgId"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type GrafanaClient struct {
	config        grafana.Config
	baseURL       url.URL
	client        *http.Client
	grafanaClient *grafana.Client
}

func NewGrafanaClient(baseURL string, cfg grafana.Config) (*GrafanaClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	if cfg.BasicAuth != nil {
		u.User = cfg.BasicAuth
	}

	tr := &http.Transport{} // Creating the transport explicitly allows for connection pooling and reuse
	cli := &http.Client{Transport: tr}

	cfg.Client = cli
	grafanaClient, err := grafana.New(baseURL, cfg)
	if err != nil {
		return nil, err
	}

	return &GrafanaClient{
		config:        cfg,
		baseURL:       *u,
		client:        cli,
		grafanaClient: grafanaClient,
	}, nil
}

func (this *GrafanaClient) GetUsername() string {
	return this.config.BasicAuth.Username()
}

// This method is missing in the grafana-api-golang-client, that's the reason why we're wrapping that client at all
func (this *GrafanaClient) GetUserOrgs(user grafana.User) ([]UserOrg, error) {
	url := this.baseURL
	url.Path = fmt.Sprintf("/api/users/%d/orgs", user.ID)
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	password, _ := this.config.BasicAuth.Password()
	req.SetBasicAuth(this.config.BasicAuth.Username(), password)
	r, err := this.client.Do(req)
	defer r.Body.Close()
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	userOrgs := make([]UserOrg, 0)
	err = json.Unmarshal(body, &userOrgs)
	if err != nil {
		return nil, err
	}
	return userOrgs, nil
}

func (this *GrafanaClient) GetAutoAssignOrgId() (int64, error) {
	url := this.baseURL
	url.Path = "/api/admin/settings"
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return 0, err
	}
	password, _ := this.config.BasicAuth.Password()
	req.SetBasicAuth(this.config.BasicAuth.Username(), password)
	r, err := this.client.Do(req)
	defer r.Body.Close()
	if err != nil {
		return 0, err
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, err
	}

	settings := make(map[string]map[string]string)
	err = json.Unmarshal(body, &settings)
	if err != nil {
		return 0, err
	}

	settingsUsers, ok := settings["users"]
	if ok {
		settingsAutoAssignOrgID, ok := settingsUsers["auto_assign_org_id"]
		if ok {
			return strconv.ParseInt(settingsAutoAssignOrgID, 10, 64)
		}
	}

	return 0, errors.New("setting users.auto_assign_org_id not found")
}

func (this *GrafanaClient) CloseIdleConnections() {
	this.client.CloseIdleConnections()
}

func (this *GrafanaClient) OrgUsers(orgID int64) ([]grafana.OrgUser, error) {
	return this.grafanaClient.OrgUsers(orgID)
}

func (this *GrafanaClient) UpdateOrgUser(orgID, userID int64, role string) error {
	return this.grafanaClient.UpdateOrgUser(orgID, userID, role)
}

func (this *GrafanaClient) AddOrgUser(orgID int64, user, role string) error {
	return this.grafanaClient.AddOrgUser(orgID, user, role)
}

func (this *GrafanaClient) RemoveOrgUser(orgID, userID int64) error {
	return this.grafanaClient.RemoveOrgUser(orgID, userID)
}

func (this *GrafanaClient) CreateUser(user grafana.User) (int64, error) {
	return this.grafanaClient.CreateUser(user)
}

func (this *GrafanaClient) Users() (users []grafana.UserSearch, err error) {
	return this.grafanaClient.Users()
}

func (this *GrafanaClient) UserUpdate(u grafana.User) error {
	return this.grafanaClient.UserUpdate(u)
}

func (this *GrafanaClient) DeleteUser(id int64) error {
	return this.grafanaClient.DeleteUser(id)
}

func (this *GrafanaClient) Orgs() ([]grafana.Org, error) {
	return this.grafanaClient.Orgs()
}

func (this *GrafanaClient) UpdateOrg(id int64, name string) error {
	return this.grafanaClient.UpdateOrg(id, name)
}

func (this *GrafanaClient) NewOrg(name string) (int64, error) {
	return this.grafanaClient.NewOrg(name)
}

func (this *GrafanaClient) Org(id int64) (grafana.Org, error) {
	return this.grafanaClient.Org(id)
}

func (this *GrafanaClient) DeleteOrg(id int64) error {
	return this.grafanaClient.DeleteOrg(id)
}

// We don't just wrap this method, we also work around the bad orgID handling of the original library and Grafana API
func (this *GrafanaClient) DataSources(org *grafana.Org) ([]*grafana.DataSource, error) {
	return this.grafanaClient.WithOrgID(org.ID).DataSources()
}

// Ditto
func (this *GrafanaClient) UpdateDataSource(org *grafana.Org, s *grafana.DataSource) error {
	return this.grafanaClient.WithOrgID(org.ID).UpdateDataSource(s)
}

// Ditto
func (this *GrafanaClient) DeleteDataSource(org *grafana.Org, id int64) error {
	return this.grafanaClient.WithOrgID(org.ID).DeleteDataSource(id)
}

// Ditto
func (this *GrafanaClient) NewDataSource(org *grafana.Org, s *grafana.DataSource) (int64, error) {
	return this.grafanaClient.WithOrgID(org.ID).NewDataSource(s)
}

// Ditto
func (this *GrafanaClient) DataSource(org *grafana.Org, id int64) (*grafana.DataSource, error) {
	return this.grafanaClient.WithOrgID(org.ID).DataSource(id)
}

// Ditto
func (this *GrafanaClient) Dashboards(org *grafana.Org) ([]grafana.FolderDashboardSearchResponse, error) {
	return this.grafanaClient.WithOrgID(org.ID).Dashboards()
}

// Ditto
func (this *GrafanaClient) NewDashboard(org *grafana.Org, dashboard grafana.Dashboard) (*grafana.DashboardSaveResponse, error) {
	return this.grafanaClient.WithOrgID(org.ID).NewDashboard(dashboard)
}
