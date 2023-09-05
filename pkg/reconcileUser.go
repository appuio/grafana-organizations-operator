package controller

import (
	"crypto/rand"
	grafana "github.com/grafana/grafana-api-golang-client"
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

func createUser(client *GrafanaClient, keycloakUser *KeycloakUser) (*grafana.User, error) {
	password, err := generatePassword()
	if err != nil {
		return nil, err
	}
	grafanaUser := grafana.User{
		Email:    keycloakUser.Email,
		Login:    keycloakUser.Username,
		Name:     keycloakUser.GetDisplayName(),
		Password: password,
	}
	grafanaUser.ID, err = client.CreateUser(grafanaUser)
	if err != nil {
		return nil, err
	}

	userOrgs, err := client.GetUserOrgs(grafanaUser)
	if err != nil {
		return nil, err
	}

	for _, userOrg := range userOrgs {
		// we immediately remove the user from the automatically assigned org because who knows what permissions the user got on that org (can't be controlled when creating the user)
		// yes this is stupid but that's how Grafana works
		err = client.RemoveOrgUser(userOrg.OrgID, grafanaUser.ID)
		if err != nil {
			return nil, err
		}
	}

	return &grafanaUser, nil
}
