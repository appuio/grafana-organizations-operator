package controller

import (
	"context"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	controlapi "github.com/appuio/control-api/apis/v1"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// Generate list of control API organizations
func getControlApiOrganizations(ctx context.Context, organizationAppuioIoClient *rest.RESTClient) ([]orgs.Organization, error) {
	controlApiOrgs := orgs.OrganizationList{}
	err := organizationAppuioIoClient.Get().Resource("Organizations").Do(ctx).Into(&controlApiOrgs)
	if err != nil {
		return nil, err
	}
	controlApiOrgsList := []orgs.Organization{}
	for _, org := range controlApiOrgs.Items {
		controlApiOrgsList = append(controlApiOrgsList, org)
	}
	return controlApiOrgsList, nil
}

// Generate map containing all control API users. Key is the user ID, value is the user object.
func getControlApiUsersMap(ctx context.Context, appuioIoClient *rest.RESTClient) (map[string]controlapi.User, error) {
	controlApiUsers := controlapi.UserList{}
	err := appuioIoClient.Get().Resource("Users").Do(ctx).Into(&controlApiUsers)
	if err != nil {
		return nil, err
	}
	appuioControlApiUsersMap := make(map[string]controlapi.User)
	for _, user := range controlApiUsers.Items {
		appuioControlApiUsersMap[user.Name] = user
	}
	return appuioControlApiUsersMap, nil
}

// Generate map containing all Grafana users grouped by organization. Key is the organization ID, value is an array of users.
func getControlApiOrganizationUsersMap(ctx context.Context, grafanaUsersMap map[string]grafana.User, appuioIoClient *rest.RESTClient) (map[string][]grafana.User, error) {
	appuioControlApiOrganizationMembers := controlapi.OrganizationMembersList{}
	err := appuioIoClient.Get().Resource("OrganizationMembers").Do(ctx).Into(&appuioControlApiOrganizationMembers)
	if err != nil {
		return nil, err
	}
	controlApiOrganizationUsersMap := make(map[string][]grafana.User)
	for _, memberlist := range appuioControlApiOrganizationMembers.Items {
		users := []grafana.User{}
		for _, userRef := range memberlist.Spec.UserRefs {
			if grafanaUser, ok := grafanaUsersMap[userRef.Name]; ok {
				users = append(users, grafanaUser)
			} else {
				klog.Warningf("Organization '%s' should have user %s but the user wasn't synced to Grafana, ignoring", memberlist.Namespace, userRef.Name)
			}
		}
		controlApiOrganizationUsersMap[memberlist.Namespace] = users
	}
	return controlApiOrganizationUsersMap, nil
}
