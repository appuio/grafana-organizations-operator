package controller

import (
	"context"
	"errors"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	grafana "github.com/grafana/grafana-api-golang-client"
	"github.com/hashicorp/go-cleanhttp"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"reflect"
	"strings"
)

func reconcileOrgSettings(org *grafana.Org, orgName string, config grafana.Config, url string, dashboard map[string]interface{}) error {
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
	klog.Infof("Organization %d OK", org.ID)
	return nil
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

func reconcileOrgBasic(grafanaOrgLookup map[string]grafana.Org, grafanaClient *grafana.Client, o orgs.Organization) (*grafana.Org, error) {
	displayName := o.Name
	if o.Spec.DisplayName != "" {
		displayName = o.Spec.DisplayName
	}
	grafanaOrgDesiredName := o.Name + " - " + displayName

	if grafanaOrg, ok := grafanaOrgLookup[o.Name]; ok {
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

func ReconcileAllOrgs(ctx context.Context, controlApiClient *rest.RESTClient, grafanaConfig grafana.Config, grafanaUrl string, dashboard map[string]interface{}) error {
	grafanaConfig.Client = cleanhttp.DefaultPooledClient()
	grafanaClient, err := grafana.New(grafanaUrl, grafanaConfig)
	if err != nil {
		return err
	}
	defer grafanaConfig.Client.CloseIdleConnections()

	appuioControlApiOrgs := orgs.OrganizationList{}
	err = controlApiClient.Get().Resource("Organizations").Do(ctx).Into(&appuioControlApiOrgs)
	if err != nil {
		return err
	}

	orgs, err := grafanaClient.Orgs()
	if err != nil {
		return err
	}

	grafanaOrgLookup := make(map[string]grafana.Org)
	for _, org := range orgs {
		nameComponents := strings.Split(org.Name, " - ")
		if len(nameComponents) < 2 || strings.Contains(nameComponents[0], " ") {
			continue
		}
		grafanaOrgLookup[nameComponents[0]] = org
	}

	for _, o := range appuioControlApiOrgs.Items {
		grafanaOrg, err := reconcileOrgBasic(grafanaOrgLookup, grafanaClient, o)
		if err != nil {
			return err
		}
		delete(grafanaOrgLookup, o.Name)

		err = reconcileOrgSettings(grafanaOrg, o.Name, grafanaConfig, grafanaUrl, dashboard)
		if err != nil {
			return err
		}

		// select with a default case is apparently the only way to do a non-blocking read from a channel
		select {
		case <-ctx.Done():
			return nil
		default:
			// carry on
		}
	}

	for _, grafanaOrgToBeDeleted := range grafanaOrgLookup {
		klog.Infof("Organization %d should not exist, deleting: '%s'", grafanaOrgToBeDeleted.ID, grafanaOrgToBeDeleted.Name)
		err = grafanaClient.DeleteOrg(grafanaOrgToBeDeleted.ID)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}

	klog.Infof("Reconcile complete")

	return nil
}
