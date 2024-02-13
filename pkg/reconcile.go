package controller

import (
	"context"
	"errors"
	"k8s.io/klog/v2"
)

type Config struct {
	GrafanaDatasourceUrl      string
	GrafanaDatasourceUsername string
	GrafanaDatasourcePassword string
	GrafanaClearAutoAssignOrg bool
}

var (
	interruptedError = errors.New("interrupted")
)

func Reconcile(ctx context.Context, config Config, keycloakClient *KeycloakClient, grafanaClient *GrafanaClient, dashboards []Dashboard) error {
	klog.Infof("Fetching Keycloak access token...")
	keycloakToken, err := keycloakClient.GetToken()
	if err != nil {
		return err
	}

	klog.Infof("Fetching users from Keycloak...")
	keycloakUsers, err := keycloakClient.GetUsers(keycloakToken)
	if err != nil {
		return err
	}
	klog.Infof("Found %d users", len(keycloakUsers))

	klog.Infof("Syncing users to Grafana...")
	keycloakUsers, err = reconcileUsers(ctx, keycloakUsers, grafanaClient)
	if err != nil {
		return err
	}
	klog.Infof("Synced %d users", len(keycloakUsers))

	klog.Infof("Fetching group memberships from Keycloak...")
	keycloakUserGroups, err := keycloakClient.GetGroupMemberships(keycloakToken, keycloakUsers)
	if err != nil {
		return err
	}
	memberships := 0
	for _, groups := range keycloakUserGroups {
		memberships += len(groups)
	}
	klog.Infof("Found %d group memberships", memberships)

	klog.Infof("Fetching organizations from Keycloak...")
	keycloakOrganizations, err := keycloakClient.GetOrganizations(keycloakToken)
	klog.Infof("Found %d organizations", len(keycloakOrganizations))

	klog.Infof("Extracting admin users...")
	var keycloakAdmins []*KeycloakUser
	var keycloakUsersWithoutAdmins []*KeycloakUser
outAdmins:
	for _, user := range keycloakUsers {
		for _, group := range keycloakUserGroups[user] {
			if group.Path == keycloakClient.adminGroupPath {
				keycloakAdmins = append(keycloakAdmins, user)
				continue outAdmins
			}
		}
		keycloakUsersWithoutAdmins = append(keycloakUsersWithoutAdmins, user)
	}
	klog.Infof("Found %d admin users", len(keycloakAdmins))

	grafanaOrgsMap, err := reconcileAllOrgs(ctx, config, keycloakOrganizations, grafanaClient, dashboards)
	if err != nil {
		return err
	}

	klog.Infof("Checking permissions of normal orgs...")
	grafanaPermissionsMap := getGrafanaPermissionsMap(keycloakUserGroups, keycloakAdmins, keycloakOrganizations)
	err = reconcilePermissions(ctx, grafanaPermissionsMap, grafanaOrgsMap, grafanaClient)
	if err != nil {
		return err
	}

	if config.GrafanaClearAutoAssignOrg {
		klog.Infof("Fetching auto_assign_org_id...")
		autoAssignOrgId, err := grafanaClient.GetAutoAssignOrgId()
		if err != nil {
			return err
		}
		klog.Infof("Removing members of auto_assign_org %d", autoAssignOrgId)
		var permissions []GrafanaPermissionSpec
		err = reconcileSingleOrgPermissions(ctx, permissions, autoAssignOrgId, grafanaClient)
		if err != nil {
			return err
		}
	}

	grafanaClient.CloseIdleConnections()
	keycloakClient.CloseIdleConnections()

	return nil
}

type GrafanaPermissionSpec struct {
	Uid            string
	PermittedRoles []string
}

// Convert group memberships found in Keycloak into permissions on organizations in Grafana
func getGrafanaPermissionsMap(keycloakUserGroups map[*KeycloakUser][]*KeycloakGroup, keycloakAdmins []*KeycloakUser, keycloakOrganizations []*KeycloakGroup) map[string][]GrafanaPermissionSpec {
	permissionsMap := make(map[string][]GrafanaPermissionSpec)
	for _, keycloakOrganization := range keycloakOrganizations {
		permissionsMap[keycloakOrganization.Name] = []GrafanaPermissionSpec{}

	userLoop:
		for keycloakUser, groups := range keycloakUserGroups {
			// If this user is an admin we ignore any specific organization permissions
			for _, admin := range keycloakAdmins {
				if admin.Username == keycloakUser.Username {
					continue userLoop
				}
			}
			for _, group := range groups {
				if keycloakOrganization.IsSameOrganization(group) {
					permissionsMap[keycloakOrganization.GetOrganizationName()] = append(permissionsMap[keycloakOrganization.GetOrganizationName()], GrafanaPermissionSpec{Uid: keycloakUser.Username, PermittedRoles: []string{"Editor", "Viewer"}})
					continue userLoop // don't try to find further permissions, otherwise we may get more than one permission for the same user on the same org
				}
			}
		}

		for _, admin := range keycloakAdmins {
			permissionsMap[keycloakOrganization.Name] = append(permissionsMap[keycloakOrganization.Name], GrafanaPermissionSpec{Uid: admin.Username, PermittedRoles: []string{"Admin", "Editor", "Viewer"}})
		}
	}
	return permissionsMap
}
