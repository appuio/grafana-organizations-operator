package controller

import (
	"context"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/klog/v2"
)

func reconcileUsers(ctx context.Context, keycloakUsers []*KeycloakUser, grafanaClient *GrafanaClient) ([]*KeycloakUser, error) {
	var syncedUsers []*KeycloakUser
	grafanaUsers, err := grafanaClient.Users()
	if err != nil {
		return nil, err
	}
	grafanaUsersMap := make(map[string]grafana.UserSearch)
	for _, grafanaUser := range grafanaUsers {
		if grafanaUser.Login != "admin" && grafanaUser.Login != grafanaClient.GetUsername() { // ignore admin
			grafanaUsersMap[grafanaUser.Login] = grafanaUser
		}
	}

	for _, keycloakUser := range keycloakUsers {
		var grafanaUser *grafana.User
		if grafanaUserSearch, ok := grafanaUsersMap[keycloakUser.Username]; ok {
			if grafanaUserSearch.Email != keycloakUser.Email ||
				grafanaUserSearch.IsAdmin ||
				grafanaUserSearch.Login != keycloakUser.Username ||
				grafanaUserSearch.Name != keycloakUser.GetDisplayName() {
				klog.Infof("User '%s' differs, fixing", keycloakUser.Username)
				grafanaUser = &grafana.User{
					ID:      grafanaUserSearch.ID,
					IsAdmin: false,
					Login:   keycloakUser.Username,
					Name:    keycloakUser.GetDisplayName(),
					Email:   keycloakUser.Email,
				}
				grafanaClient.UserUpdate(*grafanaUser)
			}
			syncedUsers = append(syncedUsers, keycloakUser)
		}
		// For now we do not create users in Grafana.
		// The original thought of this was that it would be possible to set up the users and permissions before the user logs in for the first time, therefore providing him/her with correct permissions upon first login.
		// Turns out this doesn't work, as with the first OAuth login Grafana resets all permissions of the user, breaking this entire scheme.
		// Instead now we let Grafana create the user with invalid permissions, then we go and fix the permissions.
		/*
			else {
				klog.Infof("User '%s' is missing, adding", keycloakUser.Username)
				grafanaUser, err = createUser(grafanaClient, keycloakUser)
				if err != nil {
					// for now just continue in case errors happen
					klog.Error(err)
					continue
				}
			}*/
		delete(grafanaUsersMap, keycloakUser.Username)

		select {
		case <-ctx.Done():
			return nil, interruptedError
		default:
		}
	}

	for _, grafanaUser := range grafanaUsersMap {
		klog.Infof("User '%s' (%d) not found in Keycloak, removing", grafanaUser.Login, grafanaUser.ID)
		grafanaClient.DeleteUser(grafanaUser.ID)

		select {
		case <-ctx.Done():
			return nil, interruptedError
		default:
		}
	}

	return syncedUsers, nil
}
