package controller

import (
	"errors"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/klog/v2"
	"reflect"
)

// Sync the basic org. Uses the generic Grafana client.
func reconcileOrgBasic(grafanaOrgLookup map[string]grafana.Org, grafanaClient *GrafanaClient, keycloakOrganization *KeycloakGroup) (*grafana.Org, error) {
	displayName := keycloakOrganization.Name
	if keycloakOrganization.GetDisplayNameAttribute() != "" {
		displayName = keycloakOrganization.GetDisplayNameAttribute()
	}
	grafanaOrgDesiredName := keycloakOrganization.Name + " - " + displayName

	if grafanaOrg, ok := grafanaOrgLookup[keycloakOrganization.Name]; ok {
		if grafanaOrg.Name != grafanaOrgDesiredName {
			klog.Infof("Organization %d has wrong name: '%s', should be '%s'", grafanaOrg.ID, grafanaOrg.Name, grafanaOrgDesiredName)
			err := grafanaClient.UpdateOrg(grafanaOrg.ID, grafanaOrgDesiredName)
			if err != nil {
				return nil, err
			}
		}
		return &grafanaOrg, nil
	}

	klog.Infof("Organization missing, creating: '%s'", grafanaOrgDesiredName)
	grafanaOrgId, err := grafanaClient.NewOrg(grafanaOrgDesiredName)
	if err != nil {
		return nil, err
	}
	grafanaOrg, err := grafanaClient.Org(grafanaOrgId)
	if err != nil {
		return nil, err
	}
	return &grafanaOrg, nil
}

func reconcileOrgSettings(config Config, org *grafana.Org, orgName string, grafanaClient *GrafanaClient, dashboards []Dashboard) error {
	dataSource, err := reconcileOrgDataSource(config, org, orgName, grafanaClient)
	if err != nil {
		return err
	}
	for _, dashboard := range dashboards {
		err = reconcileOrgDashboard(org, dataSource, grafanaClient, dashboard)
		if err != nil {
			return err
		}
	}
	klog.Infof("Organization %d OK", org.ID)
	return nil
}

func reconcileOrgDataSource(config Config, org *grafana.Org, orgName string, grafanaClient *GrafanaClient) (*grafana.DataSource, error) {
	secureJSONData := map[string]interface{}{
		"httpHeaderValue1": orgName,
	}
	basicAuth := config.GrafanaDatasourceUsername != ""
	if basicAuth {
		secureJSONData["basicAuthPassword"] = config.GrafanaDatasourcePassword
	}

	// If you add/remove fields here you must also adjust the 'if' statement further down
	desiredDataSource := &grafana.DataSource{
		Name:          "Mimir",
		URL:           config.GrafanaDatasourceUrl,
		BasicAuth:     basicAuth,
		BasicAuthUser: config.GrafanaDatasourceUsername,
		OrgID:         org.ID, // doesn't actually do anything, we just keep it here in case it becomes relevant with some never version of the client library. The actual orgId is taken from the 'X-Grafana-Org-Id' HTTP header which is set up via grafanaConfig.OrgID
		Type:          "prometheus",
		IsDefault:     true,
		JSONData: map[string]interface{}{
			"httpHeaderName1": "X-Scope-OrgID",
			"httpMethod":      "POST",
			"prometheusType":  "Mimir",
		},
		SecureJSONData: secureJSONData,
		Access:         "proxy",
	}

	var configuredDataSource *grafana.DataSource
	configuredDataSource = nil
	dataSources, err := grafanaClient.DataSources(org)
	if err != nil {
		return nil, err
	}
	if len(dataSources) > 0 {
		for _, dataSource := range dataSources {
			if dataSource.Name == desiredDataSource.Name {
				if dataSource.URL != desiredDataSource.URL ||
					dataSource.BasicAuth != desiredDataSource.BasicAuth ||
					dataSource.Type != desiredDataSource.Type ||
					dataSource.IsDefault != desiredDataSource.IsDefault ||
					!reflect.DeepEqual(dataSource.JSONData, desiredDataSource.JSONData) ||
					dataSource.Access != desiredDataSource.Access {
					// note that we can't detect changed basic auth credentials (BasicAuthUser, secureJSONData) because the API does not give us the current settings
					klog.Infof("Organization %d has misconfigured data source, fixing", org.ID)
					desiredDataSource.ID = dataSource.ID
					desiredDataSource.UID = dataSource.UID
					err := grafanaClient.UpdateDataSource(org, desiredDataSource)
					if err != nil {
						return nil, err
					}
					configuredDataSource = desiredDataSource
				} else {
					configuredDataSource = dataSource
				}
			} else {
				klog.Infof("Organization %d has invalid data source %d %s, removing", org.ID, dataSource.ID, dataSource.Name)
				err = grafanaClient.DeleteDataSource(org, dataSource.ID)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if configuredDataSource == nil {
		klog.Infof("Organization %d missing data source, creating", org.ID)
		dataSourceId, err := grafanaClient.NewDataSource(org, desiredDataSource)
		if err != nil {
			return nil, err
		}
		configuredDataSource, err = grafanaClient.DataSource(org, dataSourceId)
		if err != nil {
			return nil, err
		}
	}
	return configuredDataSource, nil
}

func reconcileOrgDashboard(org *grafana.Org, dataSource *grafana.DataSource, grafanaClient *GrafanaClient, dashboard Dashboard) error {
	folder, err := reconcileOrgDashboardFolder(org, grafanaClient, dashboard.Folder)
	if err != nil {
		return err
	}

	dashboardTitle, ok := dashboard.Data["title"]
	if !ok {
		return errors.New("Invalid dashboard format: 'title' key not found")
	}

	dashboards, err := grafanaClient.Dashboards(org)
	if err != nil {
		return err
	}
	for _, db := range dashboards {
		if db.FolderUID != folder.UID {
			// not in our folder, ignore
			continue
		}
		if dashboardTitle == db.Title {
			// Dashboard with this title already exists. We don't try to "fix" the dashboards for now, as this can cause various issues.
			//klog.Infof("Grafana organization %d already has dashboard '%s'", org.ID, dashboardTitle)
			return nil
		}
	}

	db := grafana.Dashboard{
		Model:     dashboard.Data,
		Overwrite: true,
		FolderUID: folder.UID,
	}
	klog.Infof("Creating dashboard '%s' for organization %d", dashboardTitle, org.ID)
	_, err = grafanaClient.NewDashboard(org, db)
	if err != nil {
		return err
	}
	return nil
}

func reconcileOrgDashboardFolder(org *grafana.Org, grafanaClient *GrafanaClient, title string) (*grafana.Folder, error) {
	folders, err := grafanaClient.Folders(org)
	if err != nil {
		return nil, err
	}
	for _, folder := range folders {
		if folder.Title == title {
			return &folder, nil
		}
	}
	folder, err := grafanaClient.NewFolder(org, title)
	if err != nil {
		return nil, err
	}
	return &folder, nil
}
