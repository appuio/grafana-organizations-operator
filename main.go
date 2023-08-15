package main

import (
	"context"
	controller "github.com/appuio/grafana-organizations-operator/pkg"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	GrafanaUrl                     string
	GrafanaUsername                string
	GrafanaPassword                string
	KeycloakUrl                    string
	KeycloakRealm                  string
	KeycloakUsername               string
	KeycloakPassword               string
	KeycloakClientId               string
	KeycloakAdminGroupPath         string
	KeycloakAutoAssignOrgGroupPath string
)

func main() {
	GrafanaUrl = os.Getenv("GRAFANA_URL")
	GrafanaUsername = os.Getenv("GRAFANA_USERNAME")
	if GrafanaUsername == "" {
		GrafanaUsername = os.Getenv("admin-user") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	GrafanaPassword = os.Getenv("GRAFANA_PASSWORD")
	GrafanaPasswordHidden := ""
	if GrafanaPassword == "" {
		GrafanaPassword = os.Getenv("admin-password") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	if GrafanaPassword != "" {
		GrafanaPasswordHidden = "***hidden***"
	}

	KeycloakUrl = os.Getenv("KEYCLOAK_URL")
	KeycloakRealm = os.Getenv("KEYCLOAK_REALM")
	KeycloakUsername = os.Getenv("KEYCLOAK_USERNAME")
	KeycloakPassword = os.Getenv("KEYCLOAK_PASSWORD")
	KeycloakClientId = os.Getenv("KEYCLOAK_CLIENT_ID")
	KeycloakPasswordHidden := ""
	if KeycloakPassword != "" {
		KeycloakPasswordHidden = "***hidden***"
	}
	KeycloakAdminGroupPath = os.Getenv("KEYCLOAK_ADMIN_GROUP_PATH")
	KeycloakAutoAssignOrgGroupPath = os.Getenv("KEYCLOAK_AUTO_ASSIGN_ORG_GROUP_PATH")

	klog.Infof("GRAFANA_URL:                         %s\n", GrafanaUrl)
	klog.Infof("GRAFANA_USERNAME:                    %s\n", GrafanaUsername)
	klog.Infof("GRAFANA_PASSWORD:                    %s\n", GrafanaPasswordHidden)
	klog.Infof("KEYCLOAK_URL:                        %s\n", KeycloakUrl)
	klog.Infof("KEYCLOAK_REALM:                      %s\n", KeycloakRealm)
	klog.Infof("KEYCLOAK_USERNAME:                   %s\n", KeycloakUsername)
	klog.Infof("KEYCLOAK_PASSWORD:                   %s\n", KeycloakPasswordHidden)
	klog.Infof("KEYCLOAK_CLIENT_ID:                  %s\n", KeycloakClientId)
	klog.Infof("KEYCLOAK_ADMIN_GROUP_PATH:           %s\n", KeycloakAdminGroupPath)
	klog.Infof("KEYCLOAK_AUTO_ASSIGN_ORG_GROUP_PATH: %s\n", KeycloakAutoAssignOrgGroupPath)

	keycloakClient, err := controller.NewKeycloakClient(KeycloakUrl, KeycloakRealm, KeycloakUsername, KeycloakPassword, KeycloakClientId, KeycloakAdminGroupPath, KeycloakAutoAssignOrgGroupPath)
	if err != nil {
		klog.Errorf("Could not create keycloakClient client: %v\n", err)
		os.Exit(1)
	}
	defer keycloakClient.CloseIdleConnections()

	grafanaConfig := grafana.Config{Client: http.DefaultClient, BasicAuth: url.UserPassword(GrafanaUsername, GrafanaPassword)}
	grafanaClient, err := controller.NewGrafanaClient(GrafanaUrl, grafanaConfig)
	if err != nil {
		klog.Errorf("Could not create Grafana client: %v\n", err)
		os.Exit(1)
	}
	defer grafanaClient.CloseIdleConnections()

	// ctx will be passed to controller to signal termination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for OS signals
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-done
		klog.Info("Exiting...")
		cancel()
	}()

	db, err := os.ReadFile("default-dashboard.json")
	if err != nil {
		klog.Errorf("Could not read default dashboard: %v\n", err)
		os.Exit(1)
	}
	dashboard := make(map[string]interface{})
	json.Unmarshal(db, &dashboard)

	klog.Info("Starting initial sync...")
	err = controller.Reconcile(ctx, keycloakClient, grafanaClient, dashboard)
	if err != nil {
		klog.Errorf("Could not do initial reconciliation: %v\n", err)
		os.Exit(1)
	}

	for {
		err = controller.Reconcile(ctx, keycloakClient, grafanaClient, dashboard)
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			os.Exit(0)
		}
		if err != nil {
			klog.Errorf("Could not reconcile (will retry): %v\n", err)
		}
	}
}
