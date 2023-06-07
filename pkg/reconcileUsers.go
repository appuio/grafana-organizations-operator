package controller

import (
	"crypto/rand"
	controlapi "github.com/appuio/control-api/apis/v1"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/klog/v2"
	"math/big"
)

func generatePassword() (string, error) {
	const voc string = "abcdfghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	len := big.NewInt(int64(len(voc)))
	pw := ""

	for i := 0; i < 32; i++ {
		index, err := rand.Int(rand.Reader, len)
		if err != nil {
			return "", err
		}
		pw = pw + string(voc[index.Uint64()])
	}
	return pw, nil
}

func createUser(client *grafana.Client, user controlapi.User) (*grafana.User, error) {
	password, err := generatePassword()
	if err != nil {
		return nil, err
	}
	grafanaUser := grafana.User{
		Email:    user.Status.Email,
		Login:    user.Name,
		Name:     user.Status.DisplayName,
		Password: password,
	}
	grafanaUser.ID, err = client.CreateUser(grafanaUser)
	if err != nil {
		return nil, err
	}
	return &grafanaUser, nil
}

func reconcileUsers(client *grafana.Client, users map[string]controlapi.User) (map[string]grafana.User, error) {
	grafanaUsers, err := client.Users()
	if err != nil {
		return nil, err
	}
	grafanaUsersSet := make(map[string]grafana.UserSearch)
	for _, grafanaUser := range grafanaUsers {
		grafanaUsersSet[grafanaUser.Login] = grafanaUser
	}

	finalGrafanaUsersMap := make(map[string]grafana.User)

	for _, user := range users {
		if grafanaUserSearch, ok := grafanaUsersSet[user.Name]; ok {
			if grafanaUserSearch.Email != user.Status.Email ||
				grafanaUserSearch.IsAdmin ||
				grafanaUserSearch.Login != user.Name ||
				grafanaUserSearch.Name != user.Status.DisplayName {
				klog.Infof("User '%s' differs, fixing", user.Name)
				grafanaUser := grafana.User{
					ID:      grafanaUserSearch.ID,
					IsAdmin: false,
					Login:   user.Name,
					Name:    user.Status.DisplayName,
				}
				client.UserUpdate(grafanaUser)
			}
			finalGrafanaUsersMap[grafanaUserSearch.Login] = grafana.User{ID: grafanaUserSearch.ID, Login: grafanaUserSearch.Login}
		} else {
			klog.Infof("User '%s' is missing, adding", user.Name)
			grafanaUser, err := createUser(client, user)
			if err != nil {
				//return err
				// for now just continue in case errors happen
				klog.Error(err)
				continue
			}
			klog.Infof("%d", grafanaUser.ID)
			finalGrafanaUsersMap[grafanaUser.Login] = grafana.User{ID: grafanaUser.ID, Login: grafanaUser.Login}
		}
		klog.Infof("User '%s' OK", user.Name)
		delete(grafanaUsersSet, user.Name)
	}

	delete(grafanaUsersSet, "admin") // don't delete the admin user...

	for _, grafanaUser := range grafanaUsersSet {
		klog.Infof("User '%s' (%d) is not in APPUiO Control API, removing", grafanaUser.Login, grafanaUser.ID)
		client.DeleteUser(grafanaUser.ID)
	}
	return finalGrafanaUsersMap, nil
}
