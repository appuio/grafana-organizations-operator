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

func reconcileOrg(grafanaOrgLookup map[string]grafana.Org, grafanaClient *grafana.Client, o orgs.Organization) error {
	displayName := o.Name
	if o.Spec.DisplayName != "" {
		displayName = o.Spec.DisplayName
	}
	grafanaOrgName := o.Name + " - " + displayName

	if grafanaOrg, ok := grafanaOrgLookup[o.Name]; ok {
		if grafanaOrg.Name != grafanaOrgName {
			klog.Infof("Grafana organization %d has wrong name: '%s', should be '%s'\n", grafanaOrg.ID, grafanaOrg.Name, grafanaOrgName)
			err := grafanaClient.UpdateOrg(grafanaOrg.ID, grafanaOrgName)
			if err != nil {
				return err
			}
		}
	} else {
		klog.Infof("Grafana organization missing, creating: '%s'\n", grafanaOrgName)
		_, err := grafanaClient.NewOrg(grafanaOrgName)
		if err != nil {
			return err
		}
	}
	return nil
}

func ReconcileAllOrgs(ctx context.Context, controlApiClient *rest.RESTClient, grafanaConfig grafana.Config, grafanaUrl string) error {
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
		err = reconcileOrg(grafanaOrgLookup, grafanaClient, o)
		if err != nil {
			return err
		}
		delete(grafanaOrgLookup, o.Name)

		// select with a default case is apparently the only way to do a non-blocking read from a channel
		select {
		case <-ctx.Done():
			return nil
		default:
			// carry on
		}
	}

	for _, grafanaOrgToBeDeleted := range grafanaOrgLookup {
		klog.Infof("Grafana organization %d should not exist, deleting: '%s'\n", grafanaOrgToBeDeleted.ID, grafanaOrgToBeDeleted.Name)
		err = grafanaClient.DeleteOrg(grafanaOrgToBeDeleted.ID)
		if err != nil {
			return err
		}
	}

	return nil
}
