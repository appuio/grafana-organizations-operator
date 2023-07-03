package controller

import (
	"context"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	grafana "github.com/grafana/grafana-api-golang-client"
	"github.com/hashicorp/go-cleanhttp"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"strings"
)

func ReconcileAllOrgs(ctx context.Context, organizationAppuioIoClient *rest.RESTClient, appuioIoClient *rest.RESTClient, grafanaConfig grafana.Config, grafanaUrl string, dashboard map[string]interface{}) error {
	// Fetch everything we need from the control API.
	// This is racy because data can change while we fetch it, making the result inconsistent. This may lead to sync errors,
	// but they should disappear with subsequent syncs.
	controlApiOrganizationsList, err := getControlApiOrganizations(ctx, organizationAppuioIoClient)
	if err != nil {
		return err
	}
	controlApiUsersMap, err := getControlApiUsersMap(ctx, appuioIoClient)
	if err != nil {
		return err
	}

	// Generic Grafana client, not specific to an org (deeper down we'll also create an org-specific Grafana client)
	grafanaConfig.Client = cleanhttp.DefaultPooledClient()
	grafanaClient, err := grafana.New(grafanaUrl, grafanaConfig)
	if err != nil {
		return err
	}
	defer grafanaConfig.Client.CloseIdleConnections()

	// Get all orgs from Grafana
	orgs, err := grafanaClient.Orgs()
	if err != nil {
		return err
	}

	// Users are a top-level resource, like organizations. Users can exist even if they don't have permissions to do anything.
	grafanaUsersMap, err := reconcileUsers(grafanaClient, controlApiUsersMap)
	if err != nil {
		return err
	}

	// Lookup table org -> users (editors or viewers)
	appuioControlApiOrganizationUsersMap, err := getControlApiOrganizationUsersMap(ctx, grafanaUsersMap, appuioIoClient)
	if err != nil {
		return err
	}

	// List of admin users (for now this is equivalent to all users of the "vshn" org). The same for all orgs.
	var desiredAdmins []grafana.User
	var ok bool
	if desiredAdmins, ok = appuioControlApiOrganizationUsersMap["vshn"]; !ok {
		desiredAdmins = []grafana.User{}
	}

	// Lookup table org ID (the one from the control API, type string) -> Grafana org
	grafanaOrgLookup := make(map[string]grafana.Org)
	for _, org := range orgs {
		nameComponents := strings.Split(org.Name, " - ")
		if len(nameComponents) < 2 || strings.Contains(nameComponents[0], " ") {
			continue
		}
		grafanaOrgLookup[nameComponents[0]] = org
	}

	// first make sure that all orgs that need to be present are present
	for _, o := range controlApiOrganizationsList {
		grafanaOrg, err := reconcileOrgBasic(grafanaOrgLookup, grafanaClient, o)
		if err != nil {
			return err
		}
		delete(grafanaOrgLookup, o.Name)

		err = reconcileOrgSettings(grafanaOrg, o.Name, grafanaConfig, grafanaUrl, dashboard, appuioControlApiOrganizationUsersMap[o.Name], desiredAdmins)
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

	// then delete the ones that shouldn't be present
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

// Sync the basic org. Uses the generic Grafana client.
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
