package controller

import (
	"context"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/klog/v2"
	"strings"
)

func reconcileAllOrgs(ctx context.Context, keycloakOrganizations []*KeycloakGroup, grafanaClient *GrafanaClient, dashboard map[string]interface{}) (map[string]*grafana.Org, error) {
	grafanaOrgLookupFinal := make(map[string]*grafana.Org)

	// Get all orgs from Grafana
	orgs, err := grafanaClient.Orgs()
	if err != nil {
		return nil, err
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
	for _, keycloakOrganization := range keycloakOrganizations {
		grafanaOrg, err := reconcileOrgBasic(grafanaOrgLookup, grafanaClient, keycloakOrganization)
		if err != nil {
			return nil, err
		}
		delete(grafanaOrgLookup, keycloakOrganization.Name)

		err = reconcileOrgSettings(grafanaOrg, keycloakOrganization.Name, grafanaClient, dashboard)
		if err != nil {
			return nil, err
		}

		grafanaOrgLookupFinal[keycloakOrganization.Name] = grafanaOrg

		// select with a default case is apparently the only way to do a non-blocking read from a channel
		select {
		case <-ctx.Done():
			return nil, interruptedError
		default:
			// carry on
		}
	}

	// then delete the ones that shouldn't be present
	for _, grafanaOrgToBeDeleted := range grafanaOrgLookup {
		klog.Infof("Organization %d should not exist, deleting: '%s'", grafanaOrgToBeDeleted.ID, grafanaOrgToBeDeleted.Name)
		err = grafanaClient.DeleteOrg(grafanaOrgToBeDeleted.ID)
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, interruptedError
		default:
		}
	}

	return grafanaOrgLookupFinal, nil
}
