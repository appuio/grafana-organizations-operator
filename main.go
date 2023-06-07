package main

import (
	"context"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	controlapi "github.com/appuio/control-api/apis/v1"
	controller "github.com/appuio/grafana-organizations-operator/pkg"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	ControlApiToken string
	ControlApiUrl   string
	GrafanaUrl      string
	GrafanaUsername string
	GrafanaPassword string
)

func main() {
	ControlApiUrl = os.Getenv("CONTROL_API_URL")
	ControlApiToken = os.Getenv("CONTROL_API_TOKEN")
	GrafanaUrl = os.Getenv("GRAFANA_URL")
	GrafanaUsername = os.Getenv("GRAFANA_USERNAME")
	if GrafanaUsername == "" {
		GrafanaUsername = os.Getenv("admin-user") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	GrafanaPassword = os.Getenv("GRAFANA_PASSWORD")
	if GrafanaPassword == "" {
		GrafanaPassword = os.Getenv("admin-password") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}

	klog.Infof("CONTROL_API_URL: %s\n", ControlApiUrl)
	klog.Infof("GRAFANA_URL: %s\n", GrafanaUrl)
	klog.Infof("GRAFANA_USERNAME: %s\n", GrafanaUsername)

	// Because of the strange design of the k8s client we actually need two client objects, which both internally use the same httpClient.
	// To make this work we also need three (!) config objects, a common one for the httpClient and one for each k8s client.
	commonConfig := &rest.Config{}
	commonConfig.BearerToken = ControlApiToken
	httpClient, err := rest.HTTPClientFor(commonConfig)
	if err != nil {
		klog.Errorf("Could not create Control API httpClient: %v\n", err)
		os.Exit(1)
	}

	organizationAppuioIoConfig := &rest.Config{}
	organizationAppuioIoConfig.Host = ControlApiUrl
	organizationAppuioIoConfig.APIPath = "/apis"
	organizationAppuioIoConfig.GroupVersion = &orgs.GroupVersion
	organizationAppuioIoConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	organizationAppuioIoClient, err := rest.RESTClientForConfigAndClient(organizationAppuioIoConfig, httpClient)
	if err != nil {
		klog.Errorf("Could not create Control API client for organization.appuio.io: %v\n", err)
		os.Exit(1)
	}

	appuioIoConfig := &rest.Config{}
	appuioIoConfig.Host = ControlApiUrl
	appuioIoConfig.APIPath = "/apis"
	appuioIoConfig.GroupVersion = &controlapi.GroupVersion
	appuioIoConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	appuioIoClient, err := rest.RESTClientForConfigAndClient(appuioIoConfig, httpClient)
	if err != nil {
		klog.Errorf("Could not connect Control API client for appuio.io: %v\n", err)
		os.Exit(1)
	}

	grafanaConfig := grafana.Config{Client: http.DefaultClient, BasicAuth: url.UserPassword(GrafanaUsername, GrafanaPassword)}

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
	err = controller.ReconcileAllOrgs(ctx, organizationAppuioIoClient, appuioIoClient, grafanaConfig, GrafanaUrl, dashboard)
	if err != nil {
		klog.Errorf("Could not do initial reconciliation: %v\n", err)
		os.Exit(1)
	}

	for {
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			os.Exit(0)
		}
		err = controller.ReconcileAllOrgs(ctx, organizationAppuioIoClient, appuioIoClient, grafanaConfig, GrafanaUrl, dashboard)
		if err != nil {
			klog.Errorf("Could not reconcile (will retry): %v\n", err)
		}
	}
}
