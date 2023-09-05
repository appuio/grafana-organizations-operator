package controller

import (
	"context"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	controlapi "github.com/appuio/control-api/apis/v1"
	"k8s.io/client-go/rest"
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
