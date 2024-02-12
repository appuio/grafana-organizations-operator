package main

import (
	"context"
	"fmt"
	controller "github.com/appuio/grafana-organizations-operator/pkg"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"
)

func main() {
	config := controller.Config{}
	grafanaUrl := os.Getenv("GRAFANA_URL")
	grafanaUsername := os.Getenv("GRAFANA_USERNAME")
	if grafanaUsername == "" {
		grafanaUsername = os.Getenv("admin-user") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	grafanaPassword := os.Getenv("GRAFANA_PASSWORD")
	grafanaPasswordHidden := ""
	if grafanaPassword == "" {
		grafanaPassword = os.Getenv("admin-password") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	if grafanaPassword != "" {
		grafanaPasswordHidden = "***hidden***"
	}
	config.GrafanaDatasourceUrl = os.Getenv("GRAFANA_DATASOURCE_URL")
	config.GrafanaDatasourceUsername = os.Getenv("GRAFANA_DATASOURCE_USERNAME")
	config.GrafanaDatasourcePassword = os.Getenv("GRAFANA_DATASOURCE_PASSWORD")
	grafanaDatasourcePasswordHidden := ""
	if config.GrafanaDatasourcePassword != "" {
		grafanaDatasourcePasswordHidden = "***hidden***"
	}
	config.GrafanaClearAutoAssignOrg = os.Getenv("GRAFANA_CLEAR_AUTO_ASSIGN_ORG") == "true"

	keycloakUrl := os.Getenv("KEYCLOAK_URL")
	keycloakRealm := os.Getenv("KEYCLOAK_REALM")
	keycloakUsername := os.Getenv("KEYCLOAK_USERNAME")
	keycloakPassword := os.Getenv("KEYCLOAK_PASSWORD")
	keycloakClientId := os.Getenv("KEYCLOAK_CLIENT_ID")
	keycloakPasswordHidden := ""
	if keycloakPassword != "" {
		keycloakPasswordHidden = "***hidden***"
	}
	keycloakAdminGroupPath := os.Getenv("KEYCLOAK_ADMIN_GROUP_PATH")

	klog.Infof("GRAFANA_URL:                         %s\n", grafanaUrl)
	klog.Infof("GRAFANA_USERNAME:                    %s\n", grafanaUsername)
	klog.Infof("GRAFANA_PASSWORD:                    %s\n", grafanaPasswordHidden)
	klog.Infof("GRAFANA_DATASOURCE_URL:              %s\n", config.GrafanaDatasourceUrl)
	klog.Infof("GRAFANA_DATASOURCE_USERNAME:         %s\n", config.GrafanaDatasourceUsername)
	klog.Infof("GRAFANA_DATASOURCE_PASSWORD:         %s\n", grafanaDatasourcePasswordHidden)
	klog.Infof("GRAFANA_CLEAR_AUTO_ASSIGN_ORG:       %t\n", config.GrafanaClearAutoAssignOrg)
	klog.Infof("KEYCLOAK_URL:                        %s\n", keycloakUrl)
	klog.Infof("KEYCLOAK_REALM:                      %s\n", keycloakRealm)
	klog.Infof("KEYCLOAK_USERNAME:                   %s\n", keycloakUsername)
	klog.Infof("KEYCLOAK_PASSWORD:                   %s\n", keycloakPasswordHidden)
	klog.Infof("KEYCLOAK_CLIENT_ID:                  %s\n", keycloakClientId)
	klog.Infof("KEYCLOAK_ADMIN_GROUP_PATH:           %s\n", keycloakAdminGroupPath)

	keycloakClient, err := controller.NewKeycloakClient(keycloakUrl, keycloakRealm, keycloakUsername, keycloakPassword, keycloakClientId, keycloakAdminGroupPath)
	if err != nil {
		klog.Errorf("Could not create keycloakClient client: %v\n", err)
		os.Exit(1)
	}
	defer keycloakClient.CloseIdleConnections()

	grafanaConfig := grafana.Config{Client: http.DefaultClient, BasicAuth: url.UserPassword(grafanaUsername, grafanaPassword)}
	grafanaClient, err := controller.NewGrafanaClient(grafanaUrl, grafanaConfig)
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

	dashboards, err := loadDashboards()
	if err != nil {
		klog.Errorf("Could not load dashboards: %v\n", err)
		os.Exit(1)
	}

	klog.Info("Starting initial sync...")
	err = controller.Reconcile(ctx, config, keycloakClient, grafanaClient, dashboards)
	if err != nil {
		klog.Errorf("Could not do initial reconciliation: %v\n", err)
		os.Exit(1)
	}

	for {
		err = controller.Reconcile(ctx, config, keycloakClient, grafanaClient, dashboards)
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

func loadDashboards() ([]controller.Dashboard, error) {
	dashboardsDir, err := os.Open("dashboards")
	if err != nil {
		return nil, err
	}

	files, err := dashboardsDir.Readdir(0)
	if err != nil {
		return nil, err
	}
	dashboardsDir.Close()

	var versions []int
	for _, v := range files {
		matched, _ := regexp.MatchString("^v[0-9]+$", v.Name())
		if v.IsDir() && matched {
			version, _ := strconv.Atoi(v.Name()[1:])
			versions = append(versions, version)
		}
	}
	sort.Ints(versions)

	latest := versions[len(versions)-1]

	versionDir, err := os.Open(fmt.Sprintf("dashboards/v%d/", latest))
	if err != nil {
		return nil, err
	}

	files, err = versionDir.Readdir(0)
	if err != nil {
		return nil, err
	}
	versionDir.Close()

	folderName := fmt.Sprintf("General v%d", latest)
	var dashboards []controller.Dashboard
	for _, file := range files {
		path := fmt.Sprintf("%s/%s", versionDir.Name(), file.Name())
		dashboardJson, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		dashboardMap := make(map[string]interface{})
		err = json.Unmarshal(dashboardJson, &dashboardMap)
		if err != nil {
			return nil, err
		}
		dashboards = append(dashboards, controller.Dashboard{Data: dashboardMap, Folder: folderName})
	}

	return dashboards, nil
}
