package controller

import (
	"errors"
	grafana "github.com/grafana/grafana-api-golang-client"
	"github.com/hashicorp/go-cleanhttp"
	"k8s.io/klog/v2"
	"reflect"
	"strings"
)

func reconcileOrgSettings(org *grafana.Org, orgName string, config grafana.Config, url string, dashboard map[string]interface{}, desiredUsers []grafana.User, desiredAdmins []grafana.User) error {
	// We can't use the grafanaClient from the overarching reconciliation loop because that client doesn't have the X-Grafana-Org-Id header set.
	// It appears that the only way to set that header is to create a new client instance.
	config.Client = cleanhttp.DefaultPooledClient()
	config.OrgID = org.ID
	grafanaClient, err := grafana.New(url, config)
	if err != nil {
		return err
	}
	defer config.Client.CloseIdleConnections()
	dataSource, err := reconcileOrgDataSource(org, orgName, grafanaClient)
	if err != nil {
		return err
	}
	err = reconcileOrgDashboard(org, dataSource, grafanaClient, dashboard)
	if err != nil {
		return err
	}
	err = reconcileOrgUsers(org, grafanaClient, desiredUsers, desiredAdmins)
	if err != nil {
		return err
	}
	klog.Infof("Organization %d OK", org.ID)
	return nil
}

func reconcileOrgDataSource(org *grafana.Org, orgName string, client *grafana.Client) (*grafana.DataSource, error) {
	// If you add/remove fields here you must also adjust the 'if' statement further down
	desiredDataSource := &grafana.DataSource{
		Name:      "Mimir",
		URL:       "http://vshn-appuio-mimir-query-frontend.vshn-appuio-mimir.svc.cluster.local:8080/prometheus",
		OrgID:     org.ID, // doesn't actually do anything, we just keep it here in case it becomes relevant with some never version of the client library. The actual orgId is taken from the 'X-Grafana-Org-Id' HTTP header which is set up via grafanaConfig.OrgID
		Type:      "prometheus",
		IsDefault: true,
		JSONData: map[string]interface{}{
			"httpHeaderName1": "X-Scope-OrgID",
			"httpMethod":      "POST",
			"prometheusType":  "Mimir",
		},
		SecureJSONData: map[string]interface{}{
			"httpHeaderValue1": orgName,
		},
		Access: "proxy",
	}

	var configuredDataSource *grafana.DataSource
	configuredDataSource = nil
	dataSources, err := client.DataSources()
	if err != nil {
		return nil, err
	}
	if len(dataSources) > 0 {
		for _, dataSource := range dataSources {
			if dataSource.Name == desiredDataSource.Name {
				if dataSource.URL != desiredDataSource.URL ||
					dataSource.Type != desiredDataSource.Type ||
					dataSource.IsDefault != desiredDataSource.IsDefault ||
					!reflect.DeepEqual(dataSource.JSONData, desiredDataSource.JSONData) ||
					dataSource.Access != desiredDataSource.Access {
					klog.Infof("Organization %d has misconfigured data source, fixing", org.ID)
					desiredDataSource.ID = dataSource.ID
					desiredDataSource.UID = dataSource.UID
					err := client.UpdateDataSource(desiredDataSource)
					if err != nil {
						return nil, err
					}
					configuredDataSource = desiredDataSource
				} else {
					configuredDataSource = dataSource
				}
			} else {
				klog.Infof("Organization %d has invalid data source %d %s, removing", org.ID, dataSource.ID, dataSource.Name)
				client.DeleteDataSource(dataSource.ID)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if configuredDataSource == nil {
		klog.Infof("Organization %d missing data source, creating", org.ID)
		dataSourceId, err := client.NewDataSource(desiredDataSource)
		if err != nil {
			return nil, err
		}
		configuredDataSource, err = client.DataSource(dataSourceId)
		if err != nil {
			return nil, err
		}
	}
	return configuredDataSource, nil
}

func reconcileOrgDashboard(org *grafana.Org, dataSource *grafana.DataSource, client *grafana.Client, dashboardModel map[string]interface{}) error {
	dashboardTitle, ok := dashboardModel["title"]
	if !ok {
		errors.New("Invalid dashboard format: 'title' key not found")
	}

	dashboards, err := client.Dashboards()
	if err != nil {
		return err
	}
	for _, dashboard := range dashboards {
		if dashboardTitle == dashboard.Title {
			// Dashboard with this title already exists. We don't try to "fix" the dashboards for now, as this can cause various issues.
			//klog.Infof("Grafana organization %d already has dashboard '%s'", org.ID, dashboardTitle)
			return nil
		}
	}

	err = configureDashboard(dashboardModel, dataSource)
	if err != nil {
		return err
	}

	dashboard := grafana.Dashboard{
		Model:     dashboardModel,
		Overwrite: true,
	}
	klog.Infof("Creating dashboard '%s' for organization %d", dashboardTitle, org.ID)
	_, err = client.NewDashboard(dashboard)
	if err != nil {
		return err
	}
	return nil
}

func reconcileOrgUsers(org *grafana.Org, client *grafana.Client, desiredUsers []grafana.User, desiredAdmins []grafana.User) error {
	orgUsers, err := client.OrgUsersCurrent()
	if err != nil {
		return err
	}
	orgUsersMap := make(map[string]grafana.OrgUser)
	for _, orgUser := range orgUsers {
		orgUsersMap[orgUser.Login] = orgUser
	}

	// Sync all non-admin users (viewer, editor)
	for _, desiredUser := range desiredUsers {
		if userListContains(desiredAdmins, desiredUser) {
			// we'll add this user later as an admin
			continue
		}
		if orgUser, ok := orgUsersMap[desiredUser.Login]; ok {
			if !strings.EqualFold(orgUser.Role, "viewer") && !strings.EqualFold(orgUser.Role, "editor") {
				klog.Infof("Organization %d user %s has invalid role %s, fixing", org.ID, desiredUser.Login, orgUser.Role)
				err = client.UpdateOrgUser(org.ID, orgUser.UserID, "editor")
				if err != nil {
					return err
				}
			}
			delete(orgUsersMap, desiredUser.Login)
		} else {
			klog.Infof("Organization %d user %s is missing, adding", org.ID, desiredUser.Login)
			err = client.AddOrgUser(org.ID, desiredUser.Login, "editor")
			if err != nil {
				return err
			}
		}
	}

	// Sync all admin users
	for _, desiredAdmin := range desiredAdmins {
		if orgUser, ok := orgUsersMap[desiredAdmin.Login]; ok {
			if !strings.EqualFold(orgUser.Role, "admin") {
				klog.Infof("Organization %d admin %s has invalid role %s, fixing", org.ID, desiredAdmin.Login, orgUser.Role)
				err = client.UpdateOrgUser(org.ID, orgUser.UserID, "admin")
				if err != nil {
					return err
				}
			}
			delete(orgUsersMap, desiredAdmin.Login)
		} else {
			klog.Infof("Organization %d admin %s is missing, adding", org.ID, desiredAdmin.Login)
			err = client.AddOrgUser(org.ID, desiredAdmin.Login, "admin")
			if err != nil {
				return err
			}
		}
	}

	delete(orgUsersMap, "admin") // don't delete the admin user...

	for _, removeUser := range orgUsersMap {
		klog.Infof("Organization %d user %s should not be there, removing", org.ID, removeUser.Login)
		err = client.RemoveOrgUser(org.ID, removeUser.UserID)
		if err != nil {
			return err
		}
	}

	return nil
}

func userListContains(userList []grafana.User, user grafana.User) bool {
	for _, entry := range userList {
		if entry.ID == user.ID && entry.Login == user.Login {
			return true
		}
	}
	return false
}
