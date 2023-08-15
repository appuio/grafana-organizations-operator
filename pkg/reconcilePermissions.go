package controller

import (
	"context"
	"errors"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
)

func reconcileSingleOrgPermissions(ctx context.Context, grafanaPermissions []GrafanaPermissionSpec, grafanaOrgId int64, grafanaClient *GrafanaClient) error {
	grafanaOrg, err := grafanaClient.Org(grafanaOrgId)
	if err != nil {
		return err
	}

	grafanaPermissionsMap := make(map[string][]GrafanaPermissionSpec)
	grafanaPermissionsMap["auto_assign_org"] = grafanaPermissions

	grafanaOrgsMap := make(map[string]*grafana.Org)
	grafanaOrgsMap["auto_assign_org"] = &grafanaOrg

	return reconcilePermissions(ctx, grafanaPermissionsMap, grafanaOrgsMap, grafanaClient)
}

func reconcilePermissions(ctx context.Context, grafanaPermissionsMap map[string][]GrafanaPermissionSpec, grafanaOrgsMap map[string]*grafana.Org, grafanaClient *GrafanaClient) error {
	for orgName, permissions := range grafanaPermissionsMap {
		grafanaOrg, ok := grafanaOrgsMap[orgName]
		if !ok {
			return errors.New("Internal error: Keycloak organization not present in Grafana. This shouldn't happen.")
		}
		initialOrgUsers, err := grafanaClient.OrgUsers(grafanaOrg.ID)
		if err != nil {
			return err
		}

		for _, permission := range permissions {
			var desiredOrgUser *grafana.OrgUser

			for i, ou := range initialOrgUsers {
				if ou.Login == permission.Uid {
					desiredOrgUser = &ou
					// remove user from initialOrgUsers array
					initialOrgUsers[i] = initialOrgUsers[len(initialOrgUsers)-1]
					initialOrgUsers = initialOrgUsers[:len(initialOrgUsers)-1]
					break
				}
			}

			if desiredOrgUser == nil {
				klog.Infof("User '%s' should have access to org '%s' (%d), adding", permission.Uid, grafanaOrg.Name, grafanaOrg.ID)
				err := grafanaClient.AddOrgUser(grafanaOrg.ID, permission.Uid, permission.PermittedRoles[0])
				if err != nil {
					// This can happen due to race conditions, hence just a warning
					klog.Warning(err)
				}
			} else {
				// orgUser already exists, check if permission is acceptable
				if !slices.Contains(permission.PermittedRoles, desiredOrgUser.Role) {
					klog.Infof("User '%s' has invalid role on org '%s' (%d), fixing", permission.Uid, grafanaOrg.Name, grafanaOrg.ID)
					err := grafanaClient.UpdateOrgUser(grafanaOrg.ID, desiredOrgUser.UserID, permission.PermittedRoles[0])
					if err != nil {
						// This can happen due to race conditions, hence just a warning
						klog.Warning(err)
					}
				}
			}

			select {
			case <-ctx.Done():
				return interruptedError
			default:
			}
		}

		for _, undesiredOrgUser := range initialOrgUsers {
			if undesiredOrgUser.Login == "admin" || undesiredOrgUser.Login == grafanaClient.GetUsername() {
				continue
			}
			klog.Infof("User '%s' (%d) must not have access to org '%s' (%d), removing", undesiredOrgUser.Login, undesiredOrgUser.UserID, grafanaOrg.Name, grafanaOrg.ID)
			err := grafanaClient.RemoveOrgUser(grafanaOrg.ID, undesiredOrgUser.UserID)
			if err != nil {
				// This can happen due to race conditions, hence just a warning
				klog.Warning(err)
			}

			select {
			case <-ctx.Done():
				return interruptedError
			default:
			}
		}
	}

	return nil
}
