# Grafana Organizations Operator

Automatically set up Grafana organizations based on information in Keycloak.

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

The Grafana Organizations Operator only cares about organizations present in Keycloak (plus `auto_assign_org_id`). This allows other Organizations in Grafana to exist without being touched; but it also means that it will not clean up organizations in Grafana which have been deleted in Keycloak.

### Issues with the Keycloak API

* The Keycloak LDAP integration is [buggy when it comes to listing members of a group](https://github.com/keycloak/keycloak/issues/10348) (API endpoint `/auth/admin/realms/[REALM]/groups/[ID]/members`). This would not be an issue for the organization users but it is an issue for the `KEYCLOAK_ADMIN_GROUP_PATH` which may come from LDAP. As a workaround we fetch the group memberships of users instead, however this means one HTTP call per user (easily 100s of HTTP calls).
* The operator assumes that all Grafana organizations are in the root group `/organizations`

### Issues with Grafana

* When a user is created in Grafana, Grafana's `auto_assign_org` "feature" automatically gives the user permission to some organization (whichever is configured). This is almost never what we want. To work around this:
  * It would be possible to disable `auto_assign_org`, but then Grafana would create a new organization for every new user, which would be even worse.
  * We can't create the user and assign the correct permissions before the user logs in for the first time, because upon oauth login Grafana resets all permissions.
  * Therefore we just fix permissions after the user has been created by Grafana. This leaves a time gap during which the user can have permissions he shouldn't have, but there isn't much we can do against that.
  * A possible improvement would be to configure Grafana such that `auto_assign_org_id` points to a completely empty org, that way the invalid permissions wouldn't matter, but this isn't something this operator can configure.
* Grafana may make it impossible in the future to change permissions of OAuth users via the API. Today it is already impossible via the UI. This would break the operator in its current form entirely.
* Because the `grafana-api-golang-client` implementation is incomplete we are wrapping it in the GrafanaClient type and add some functionality.
* The Grafana API often ignores the OrgID JSON field. The only workaround for this is to set the HTTP header `x-grafana-org-id`. The GrafanaClient wrapper takes care of this.

## Development Environment Setup

In order to develop the operator you need:

* Read access to Keycloak for all users and groups
* A [Grafana test instance](https://operator-dev-grafana.apps.cloudscale-lpg-2.appuio.cloud/) to write to

You can run the `gen-dev-env.sh` to set up an environment file (`env`) with the required configuration.

Once that's done you can source the env file (`. ./env`) and run the operator on your local machine using `go run .`.

Note that by default the operator will not sync any users, as it expects the users to be created by Grafana first. You can simulate this by manually creating a user in Grafana using the `admin` account.

## License

[BSD-3-Clause](LICENSE)
