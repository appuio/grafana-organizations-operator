#!/bin/bash
set -e -o pipefail

TMP=$(mktemp -d)
git clone https://github.com/kubernetes-monitoring/kubernetes-mixin "${TMP}"
GOBIN="${TMP}/tmp/bin/" go install github.com/google/go-jsonnet/cmd/jsonnet@latest
GOBIN="${TMP}/tmp/bin/" go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest
(cd "${TMP}"; make dashboards_out)

PREV_VERSION="$(ls -1 dashboards/ | cut -c2- | sort -n | tail -n 1)"
VERSION=$(($PREV_VERSION + 1))
DST="dashboards/v${VERSION}"
mkdir "${DST}"

cp "${TMP}/dashboards_out/k8s-resources-namespace.json" "${DST}"
cp "${TMP}/dashboards_out/k8s-resources-pod.json" "${DST}"
cp "${TMP}/dashboards_out/k8s-resources-workload.json" "${DST}"
cp "${TMP}/dashboards_out/k8s-resources-workloads-namespace.json" "${DST}"

rm -rf "${TMP}"
echo ""
echo "New dashboards in ${DST}"
diff -r "dashboards/v${PREV_VERSION}" "${DST}" && echo "No difference to previous version"
