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

func reconcileOrgSettings(org *grafana.Org, orgName string, grafanaClient *GrafanaClient, dashboard map[string]interface{}) error {
	dataSource, err := reconcileOrgDataSource(org, orgName, grafanaClient)
	if err != nil {
		return err
	}
	err = reconcileOrgDashboard(org, dataSource, grafanaClient, dashboard)
	if err != nil {
		return err
	}
	klog.Infof("Organization %d OK", org.ID)
	return nil
}

func reconcileOrgDataSource(org *grafana.Org, orgName string, grafanaClient *GrafanaClient) (*grafana.DataSource, error) {
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
	dataSources, err := grafanaClient.DataSources(org)
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
				grafanaClient.DeleteDataSource(org, dataSource.ID)
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

func reconcileOrgDashboard(org *grafana.Org, dataSource *grafana.DataSource, grafanaClient *GrafanaClient, dashboardModel map[string]interface{}) error {
	dashboardTitle, ok := dashboardModel["title"]
	if !ok {
		errors.New("Invalid dashboard format: 'title' key not found")
	}

	dashboards, err := grafanaClient.Dashboards(org)
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
	_, err = grafanaClient.NewDashboard(org, dashboard)
	if err != nil {
		return err
	}
	return nil
}
