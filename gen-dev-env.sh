#!/bin/bash
set -e -o pipefail
SECRET="$(kubectl --as cluster-admin -n vshn-appuio-grafana get secret grafana-organizations-operator -ojson)" || (>&2 echo "You must be logged in to 'APPUiO Cloud LPG 2' with cluster-admin permissions for this to work" && exit 1)
echo -n "" > env
for VAL in KEYCLOAK_ADMIN_GROUP_PATH KEYCLOAK_AUTO_ASSIGN_ORG_GROUP_PATH KEYCLOAK_CLIENT_ID KEYCLOAK_PASSWORD KEYCLOAK_REALM KEYCLOAK_URL KEYCLOAK_USERNAME; do
	echo -n "export ${VAL}=\"" >> env
	echo "${SECRET}" | jq -r ".data.${VAL}" | base64 -d >> env
	echo "\"" >> env
done
echo "export GRAFANA_URL=\"https://operator-dev-grafana.apps.cloudscale-lpg-2.appuio.cloud\"" >> env
echo "export GRAFANA_USERNAME=\"admin\"" >> env
echo -n "export GRAFANA_PASSWORD=\"" >> env
kubectl --as cluster-admin -n vshn-grafana-organizations-operator-dev get secret grafana-env -ojsonpath='{.data.GF_SECURITY_ADMIN_PASSWORD}' | base64 -d >> env
echo "\"" >> env
echo "export GRAFANA_DATASOURCE_URL=\"http://vshn-appuio-mimir-nginx.vshn-appuio-mimir.svc.cluster.local/prometheus\"" >> env
echo "export GRAFANA_DATASOURCE_USERNAME=\"dummyuser\"" >> env
echo "export GRAFANA_DATASOURCE_PASSWORD=\"dummypass\"" >> env
