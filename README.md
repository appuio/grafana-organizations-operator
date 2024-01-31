# Grafana Organizations Operator

Automatically set up Grafana organizations based on information in Keycloak.

## Features

* The organizations-operator creates an organization for each Keycloak subgroup of "organizations/".
* The operator creates a Mimir data source for each organization. The data source uses the "X-Scope-OrgID" header to make sure the data returned by Mimir is scoped to the organization only.
* The operator sets up a set of default dashboards for each organization
* For every user that exists in Grafana the operator will set up the permissions according to group memberships in Keycloak. See "Design" for more details.

## Design

### Data in Keycloak

Keycloak holds the APPUiO Cloud organization and user data.

* APPUiO Cloud organizations are represented as groups with the group path `/organizations/[ORGNAME]`
* Teams within organizations are represented as groups with the path `/organizations/[ORGNAME]/[TEAMNAME]`.
* APPUiO Cloud users are represented as normal Keycloak users. All Keycloak users are potential APPUiO Cloud users.
* User permissions are represented as regular Keycloak group memberships. A user can be a member of an organization or of a team, and can have multiple partially overlapping memberships.
* All members of the group configured via `KEYCLOAK_ADMIN_GROUP_PATH` are considered to be admins and have "Admin" permissions on all organizations.
* All members of the group configured via `KEYCLOAK_AUTO_ASSIGN_ORG_GROUP_PATH` are considered to be members of the Grafana organization configured via `auto_assign_org_id`. See below for more details.

The Grafana Organizations Operator does not differentiate between organization and team membership, and does not support team-specific permissions.

This information is translated into Grafana organizations, users and organization users ("permissions" a user has on an organization).

The Grafana Organizations Operator deletes all Grafana organizations that aren't present Keycloak (except `auto_assign_org_id`). 

### Issues with Grafana

* Grafana likes to wipe all organization permissions of the user upon OAuth login. There is a configuration which prevents this:
  * `role_attribute_path: Deny` to not give the user signing in any permissions
  * `role_attribute_strict: false` to allow users without permissions to log in
  * `skip_org_role_sync: true` to make it possible to configure permissions via the UI (may not be absolutely required since we change permissions via API, but we set this for good measure).
* When a user is created in Grafana, Grafana's `auto_assign_org` "feature" automatically gives the user permission to the configured organization. This is almost never what we want. To work around this:
  * It would be possible to disable `auto_assign_org`, but then Grafana would create a new organization for every new user, which would be even worse.
  * We could create the user and assign the correct permissions before the user logs in for the first time, but that would mean having 1000s of users in Grafana which are never used.
  * Therefore we just fix permissions after the user has been created by Grafana. This leaves a time gap during which the user can have permissions he shouldn't have, but there isn't much we can do against that.
  * A possible improvement would be to configure Grafana such that `auto_assign_org_id` points to a completely empty org, that way the invalid permissions wouldn't matter, but this isn't something this operator can configure.
* Because the `grafana-api-golang-client` implementation is incomplete we are wrapping it in the GrafanaClient type and add some functionality.
* The Grafana API often ignores the OrgID JSON field. The only workaround for this is to set the HTTP header `x-grafana-org-id`. The GrafanaClient wrapper takes care of this.

### Managing Dashboards

The dashboard json needs to be put into the `dashboards/v[X]` directory and will be picked up from there.

The operator does not modify/update existing dashboards, it only installs new/missing dashboards. This is because Grafana may change dashboards (users may change them or Grafana might migrate them during a version upgrade), and we don't want the operator to revert those changes. In order to still be able to install newer versions of dashboards, the dashboard folder in the Grafana UI is versioned ("General v[X]") with the version number corresponding to the directory name in `dashboards`. Hence to add new versions of dashboards you create a `v[X+1]` directory inside `dashboards` and put them there.

The operator does not remove old dashboard versions; both old and current versions remain available to Grafana users. This may change in the future.

The suggested dashboards to use are the "kubernetes-mixin" dashboards. There is a helper script `get-k8s-mixins-sh` which downloads and builds the latest dashboard version for you; once that's done you need to rebuild the operator image and roll it out.

## Development Environment Setup

In order to develop the operator you need:

* Read access to Keycloak for all users and groups
* A [Grafana test instance](https://operator-dev-grafana.apps.cloudscale-lpg-2.appuio.cloud/) to write to

You can run the `gen-dev-env.sh` to set up an environment file (`env`) with the required configuration.

Once that's done you can source the env file (`. ./env`) and run the operator on your local machine using `go run .`.

Note that by default the operator will not sync any users, as it expects the users to be created by Grafana first.

## License

[BSD-3-Clause](LICENSE)
