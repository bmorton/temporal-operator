+++
title = "CRD Reference"
weight = 70
+++

# API Reference

## Packages
- [temporal.bmor10.com/v1alpha1](#temporalbmor10comv1alpha1)


## temporal.bmor10.com/v1alpha1

Conversion / hub-and-spoke notes:

v1alpha1 is currently the one and only API version, and is marked as the
storage version (see the +kubebuilder:storageversion markers on the root
types). When a v1beta1 is introduced, v1alpha1 will become the conversion
"hub": all spoke versions convert to and from it, and conversion webhooks
will be wired in here (via the conversion.Convertible / conversion.Hub
interfaces from sigs.k8s.io/controller-runtime). Keeping this groundwork
explicit now ensures the storage version is unambiguous and that adding a
new version later is a localized change.

Package v1alpha1 contains API Schema definitions for the temporal v1alpha1 API group.

### Resource Types
- [TemporalCluster](#temporalcluster)
- [TemporalClusterClient](#temporalclusterclient)
- [TemporalClusterConnection](#temporalclusterconnection)
- [TemporalClusterProxy](#temporalclusterproxy)
- [TemporalDevServer](#temporaldevserver)
- [TemporalNamespace](#temporalnamespace)
- [TemporalSchedule](#temporalschedule)
- [TemporalSearchAttribute](#temporalsearchattribute)



#### ArchivalSpec



ArchivalSpec is a passthrough for cluster-wide archival configuration.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `history` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ |  |  | Optional: \{\} <br /> |
| `visibility` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ |  |  | Optional: \{\} <br /> |


#### AuthorizationSpec



AuthorizationSpec configures the frontend authorizer, claim mapper, and JWT
key provider used to validate inbound bearer tokens.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `authorizer` _string_ | Authorizer selects the Temporal authorizer plugin. If unset, it defaults<br />to "default" (per-namespace RBAC) when JWT validation is configured.<br />Set it to "" to select the no-op (allow-all) authorizer for<br />authenticate-only mode. |  | Optional: \{\} <br /> |
| `claimMapper` _string_ | ClaimMapper is the Temporal claim mapper. Defaults to "default" when JWT<br />validation is configured. |  | Optional: \{\} <br /> |
| `permissionsClaimName` _string_ | PermissionsClaimName maps to global.authorization.permissionsClaimName.<br />Defaults to "roles" when Entra is set, otherwise "permissions". |  | Optional: \{\} <br /> |
| `jwtKeyProvider` _[JWTKeyProviderSpec](#jwtkeyproviderspec)_ | JWTKeyProvider configures JWKS-based token signature validation. |  | Optional: \{\} <br /> |
| `entra` _[EntraAuthSpec](#entraauthspec)_ | Entra derives the Entra JWKS keySourceURI from a tenant ID and applies<br />sensible JWT defaults. |  | Optional: \{\} <br /> |
| `config` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Config is a passthrough merged into the authorization block for any knob<br />not modeled above. |  | Optional: \{\} <br /> |


#### AzureWorkloadIdentitySpec



AzureWorkloadIdentitySpec configures passwordless Microsoft Entra auth for a
cluster's SQL datastores via Azure Workload Identity. The operator expands it
into a ServiceAccount, token sidecar/initContainers, and passwordCommand
wiring in the cluster's namespace.



_Appears in:_
- [PersistenceSpec](#persistencespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clientId` _string_ | ClientID is the Azure managed-identity / app-registration client ID used<br />for the ServiceAccount's azure.workload.identity/client-id annotation. |  |  |
| `scope` _string_ | Scope is the Entra token scope requested for the database. Defaults to<br />"https://ossrdbms-aad.database.windows.net/.default". |  | Optional: \{\} <br /> |
| `serviceAccountName` _string_ | ServiceAccountName overrides the generated ServiceAccount name<br />(default "<cluster>-azure"). |  | Optional: \{\} <br /> |
| `image` _string_ | Image overrides the azure-cli image used by the token sidecar /<br />initContainers (default "mcr.microsoft.com/azure-cli:2.87.0"). |  | Optional: \{\} <br /> |
| `refreshInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RefreshInterval is how often the server-pod sidecar refreshes the token<br />(default 30m). |  | Optional: \{\} <br /> |


#### CalendarRange



CalendarRange is an inclusive [Start,End] range with an optional Step.



_Appears in:_
- [StructuredCalendarSpec](#structuredcalendarspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `start` _integer_ |  |  |  |
| `end` _integer_ |  |  | Optional: \{\} <br /> |
| `step` _integer_ |  | 1 | Optional: \{\} <br /> |


#### CassandraDatastoreSpec



CassandraDatastoreSpec configures a Cassandra datastore.



_Appears in:_
- [DatastoreSpec](#datastorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `hosts` _string array_ |  |  | MinItems: 1 <br /> |
| `port` _integer_ |  | 9042 | Maximum: 65535 <br />Minimum: 1 <br /> |
| `keyspace` _string_ |  |  |  |
| `user` _string_ |  |  | Optional: \{\} <br /> |
| `passwordSecretRef` _[SecretKeyReference](#secretkeyreference)_ |  |  | Optional: \{\} <br /> |
| `datacenter` _string_ |  |  | Optional: \{\} <br /> |
| `replicationFactor` _integer_ |  | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `tls` _[DatastoreTLSSpec](#datastoretlsspec)_ |  |  | Optional: \{\} <br /> |


#### CertificateAuthoritySpec



CertificateAuthoritySpec configures a certificate authority.



_Appears in:_
- [MTLSSpec](#mtlsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ |  |  | Optional: \{\} <br /> |
| `duration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |


#### ClusterConnectionPeer



ClusterConnectionPeer identifies one cluster in a replication group. Exactly
one of ClusterRef or FrontendAddress must be set.



_Appears in:_
- [TemporalClusterConnectionSpec](#temporalclusterconnectionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the replication-group cluster name (== clusterMetadata.currentClusterName). |  |  |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef points at a local TemporalCluster CR. The operator resolves its<br />frontend address and reuses its CA automatically. |  | Optional: \{\} <br /> |
| `frontendAddress` _string_ | FrontendAddress is an external peer's gRPC frontend address (host:port). |  | Optional: \{\} <br /> |
| `tlsSecretRef` _[SecretReference](#secretreference)_ | TLSSecretRef supplies mTLS material for an external peer. Ignored for<br />ClusterRef peers (the cluster CA is reused). |  | Optional: \{\} <br /> |
| `enableConnection` _boolean_ | EnableConnection toggles replication traffic without removing the peer. | true | Optional: \{\} <br /> |


#### ClusterMetadataSpec



ClusterMetadataSpec configures multi-cluster replication.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enableGlobalNamespace` _boolean_ |  |  | Optional: \{\} <br /> |
| `failoverVersionIncrement` _integer_ |  |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `currentClusterName` _string_ |  |  | MinLength: 1 <br />Optional: \{\} <br /> |
| `initialFailoverVersion` _integer_ |  |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `masterClusterName` _string_ |  |  | MinLength: 1 <br />Optional: \{\} <br /> |


#### ClusterReference



ClusterReference points at a Temporal frontend in the same Kubernetes
namespace: either a TemporalCluster (default) or a TemporalDevServer.



_Appears in:_
- [ClusterConnectionPeer](#clusterconnectionpeer)
- [ProxyPeer](#proxypeer)
- [TemporalClusterClientSpec](#temporalclusterclientspec)
- [TemporalClusterProxySpec](#temporalclusterproxyspec)
- [TemporalNamespaceSpec](#temporalnamespacespec)
- [TemporalScheduleSpec](#temporalschedulespec)
- [TemporalSearchAttributeSpec](#temporalsearchattributespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the referenced object. |  |  |
| `kind` _string_ | Kind selects the referenced object type. | TemporalCluster | Enum: [TemporalCluster TemporalDevServer] <br />Optional: \{\} <br /> |


#### DatastoreSpec



DatastoreSpec configures a single datastore. Exactly one backend should be set.



_Appears in:_
- [PersistenceSpec](#persistencespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `sql` _[SQLDatastoreSpec](#sqldatastorespec)_ |  |  | Optional: \{\} <br /> |
| `cassandra` _[CassandraDatastoreSpec](#cassandradatastorespec)_ |  |  | Optional: \{\} <br /> |
| `elasticsearch` _[ElasticsearchDatastoreSpec](#elasticsearchdatastorespec)_ |  |  | Optional: \{\} <br /> |
| `schemaVersion` _string_ | SchemaVersion is either "auto" (operator-managed migrations) or a pinned<br />schema version string. | auto | Optional: \{\} <br /> |


#### DatastoreTLSSpec



DatastoreTLSSpec configures TLS for a datastore connection.



_Appears in:_
- [CassandraDatastoreSpec](#cassandradatastorespec)
- [ElasticsearchDatastoreSpec](#elasticsearchdatastorespec)
- [SQLDatastoreSpec](#sqldatastorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | true |  |
| `caSecretRef` _[SecretKeyReference](#secretkeyreference)_ |  |  | Optional: \{\} <br /> |
| `certSecretRef` _[SecretKeyReference](#secretkeyreference)_ |  |  | Optional: \{\} <br /> |
| `keySecretRef` _[SecretKeyReference](#secretkeyreference)_ |  |  | Optional: \{\} <br /> |
| `enableHostVerification` _boolean_ |  |  | Optional: \{\} <br /> |
| `serverName` _string_ |  |  | Optional: \{\} <br /> |


#### DevServerEndpoints



DevServerEndpoints reports the dev server's resolved endpoints.



_Appears in:_
- [TemporalDevServerStatus](#temporaldevserverstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `frontend` _string_ | Frontend is the gRPC frontend endpoint (host:7233). |  | Optional: \{\} <br /> |
| `ui` _string_ | UI is the Web UI endpoint (host:8233). |  | Optional: \{\} <br /> |


#### DevServerStorageSpec



DevServerStorageSpec configures SQLite storage.



_Appears in:_
- [TemporalDevServerSpec](#temporaldevserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type selects ephemeral (emptyDir, wiped on restart) or Persistent (PVC). | Ephemeral | Enum: [Ephemeral Persistent] <br />Optional: \{\} <br /> |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#quantity-resource-api)_ | Size is the PVC size when Type=Persistent. Default "1Gi". |  | Optional: \{\} <br /> |
| `storageClassName` _string_ | StorageClassName is the PVC storage class when Type=Persistent. |  | Optional: \{\} <br /> |


#### DevServerUISpec



DevServerUISpec controls the bundled Web UI.



_Appears in:_
- [TemporalDevServerSpec](#temporaldevserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled toggles the bundled Web UI. Default true. | true |  |


#### DynamicConfigConstraints



DynamicConfigConstraints scopes a dynamic config value.



_Appears in:_
- [DynamicConfigValue](#dynamicconfigvalue)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ |  |  | Optional: \{\} <br /> |
| `taskQueueName` _string_ |  |  | Optional: \{\} <br /> |
| `taskQueueType` _string_ |  |  | Optional: \{\} <br /> |


#### DynamicConfigSpec



DynamicConfigSpec is a passthrough for Temporal's dynamic configuration.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `values` _object (keys:string, values:[DynamicConfigValue](#dynamicconfigvalue))_ | Values maps a dynamic config key to one or more constrained values. |  | Optional: \{\} <br /> |


#### DynamicConfigValue



DynamicConfigValue is a single dynamic config value with optional constraints.



_Appears in:_
- [DynamicConfigSpec](#dynamicconfigspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `value` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Value is an arbitrary JSON value for the dynamic config key. |  |  |
| `constraints` _[DynamicConfigConstraints](#dynamicconfigconstraints)_ |  |  | Optional: \{\} <br /> |


#### ElasticsearchDatastoreSpec



ElasticsearchDatastoreSpec configures an Elasticsearch visibility store.



_Appears in:_
- [DatastoreSpec](#datastorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ |  |  |  |
| `version` _string_ |  | v8 | Enum: [v7 v8] <br /> |
| `username` _string_ |  |  | Optional: \{\} <br /> |
| `passwordSecretRef` _[SecretKeyReference](#secretkeyreference)_ |  |  | Optional: \{\} <br /> |
| `indices` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `tls` _[DatastoreTLSSpec](#datastoretlsspec)_ |  |  | Optional: \{\} <br /> |


#### EndpointsStatus



EndpointsStatus reports resolved cluster endpoints.



_Appears in:_
- [TemporalClusterStatus](#temporalclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `frontend` _string_ |  |  | Optional: \{\} <br /> |
| `ui` _string_ |  |  | Optional: \{\} <br /> |
| `metrics` _string_ |  |  | Optional: \{\} <br /> |


#### EntraAuthSpec



EntraAuthSpec is a Microsoft Entra convenience for server JWT validation.



_Appears in:_
- [AuthorizationSpec](#authorizationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tenantID` _string_ | TenantID is the Entra (Azure AD) tenant. Derives the JWKS keySourceURI<br />https://login.microsoftonline.com/\{tenantID\}/discovery/v2.0/keys. |  | MinLength: 1 <br /> |


#### EntraUIAuthSpec



EntraUIAuthSpec is a Microsoft Entra convenience for UI OIDC login.



_Appears in:_
- [UIAuthSpec](#uiauthspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tenantID` _string_ |  |  | MinLength: 1 <br /> |


#### FrontendMTLSSpec



FrontendMTLSSpec configures the frontend certificate.



_Appears in:_
- [MTLSSpec](#mtlsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ |  |  | Optional: \{\} <br /> |
| `dnsNames` _string array_ |  |  | Optional: \{\} <br /> |


#### InternalFrontendSpec



InternalFrontendSpec configures the optional internal-frontend service.



_Appears in:_
- [ServicesSpec](#servicesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | false |  |
| `replicas` _integer_ |  | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcerequirements-v1-core)_ |  |  | Optional: \{\} <br /> |


#### IntervalSpec



IntervalSpec matches times of epoch + n*Every + Offset.



_Appears in:_
- [ScheduleSpec](#schedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `every` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  |  |
| `offset` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |


#### IssuerReference



IssuerReference references a cert-manager Issuer or ClusterIssuer.



_Appears in:_
- [MTLSSpec](#mtlsspec)
- [ProxyMuxTLS](#proxymuxtls)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `kind` _string_ |  | Issuer | Enum: [Issuer ClusterIssuer] <br />Optional: \{\} <br /> |
| `group` _string_ |  | cert-manager.io | Optional: \{\} <br /> |


#### JWTKeyProviderSpec



JWTKeyProviderSpec configures JWKS-based JWT validation.



_Appears in:_
- [AuthorizationSpec](#authorizationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `keySourceURIs` _string array_ | KeySourceURIs are JWKS endpoints used to validate token signatures. |  | Optional: \{\} <br /> |
| `refreshInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RefreshInterval controls how often keys are refreshed, e.g. "1m". |  | Optional: \{\} <br /> |


#### MTLSSpec



MTLSSpec configures mutual TLS for the cluster.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _string_ | Provider selects the certificate provider. | cert-manager | Enum: [cert-manager] <br /> |
| `issuerRef` _[IssuerReference](#issuerreference)_ | IssuerRef references the cert-manager issuer used to mint certificates.<br />Required when provider is cert-manager. |  | Optional: \{\} <br /> |
| `internodeCA` _[CertificateAuthoritySpec](#certificateauthorityspec)_ | InternodeCA configures the internode certificate authority. |  | Optional: \{\} <br /> |
| `frontend` _[FrontendMTLSSpec](#frontendmtlsspec)_ | Frontend configures the frontend certificate. |  | Optional: \{\} <br /> |
| `refreshInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RefreshInterval is the certificate refresh interval. | 720h | Optional: \{\} <br /> |
| `renewBefore` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RenewBefore is how long before expiry a certificate is renewed. | 240h | Optional: \{\} <br /> |


#### MetricsSpec



MetricsSpec configures Prometheus integration.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | true |  |
| `port` _integer_ |  | 9090 | Maximum: 65535 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `serviceMonitor` _[ServiceMonitorSpec](#servicemonitorspec)_ |  |  | Optional: \{\} <br /> |


#### NamespaceReplicationStatus



NamespaceReplicationStatus reports the observed replication state.



_Appears in:_
- [TemporalNamespaceStatus](#temporalnamespacestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `isGlobal` _boolean_ |  |  | Optional: \{\} <br /> |
| `activeCluster` _string_ |  |  | Optional: \{\} <br /> |
| `clusters` _string array_ |  |  | Optional: \{\} <br /> |
| `failoverInProgress` _boolean_ |  |  | Optional: \{\} <br /> |
| `lastFailoverTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ |  |  | Optional: \{\} <br /> |


#### PeerConnectionStatus



PeerConnectionStatus reports the observed state of one peer.



_Appears in:_
- [TemporalClusterConnectionStatus](#temporalclusterconnectionstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `reachable` _boolean_ | Reachable is true when the operator could connect to this peer's frontend. |  | Optional: \{\} <br /> |
| `connected` _boolean_ | Connected is true when this peer appears as an enabled remote cluster on<br />the other reachable peers. |  | Optional: \{\} <br /> |
| `message` _string_ |  |  | Optional: \{\} <br /> |


#### PersistenceSpec



PersistenceSpec configures the default and visibility datastores.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `defaultStore` _[DatastoreSpec](#datastorespec)_ | DefaultStore holds workflow execution state. Exactly one of sql or<br />cassandra must be set. |  |  |
| `visibilityStore` _[DatastoreSpec](#datastorespec)_ | VisibilityStore holds visibility records. One of sql, cassandra, or<br />elasticsearch must be set. |  |  |
| `schemaJob` _[SchemaJobSpec](#schemajobspec)_ | SchemaJob customizes the schema setup/update Jobs the operator runs. |  | Optional: \{\} <br /> |
| `azureWorkloadIdentity` _[AzureWorkloadIdentitySpec](#azureworkloadidentityspec)_ | AzureWorkloadIdentity, when set, makes this cluster authenticate to its<br />SQL datastore(s) passwordlessly using Azure Workload Identity. The operator<br />generates a ServiceAccount, token sidecar/initContainers, and the<br />passwordCommand wiring in the cluster's namespace; the operator itself<br />holds no database credential. SQL stores only. |  | Optional: \{\} <br /> |


#### PersistenceStatus



PersistenceStatus reports datastore reachability and schema state.



_Appears in:_
- [TemporalClusterStatus](#temporalclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `schemaVersions` _object (keys:string, values:string)_ | SchemaVersions maps a store name to its observed schema version. |  | Optional: \{\} <br /> |
| `history` _[SchemaUpgradeRecord](#schemaupgraderecord) array_ | History records schema upgrades applied by the operator. |  | Optional: \{\} <br /> |
| `reachable` _boolean_ | Reachable indicates whether the datastores were reachable at last reconcile. |  | Optional: \{\} <br /> |


#### PodTemplateOverride



PodTemplateOverride carries metadata and a strategic-merge pod spec override.



_Appears in:_
- [SchemaJobSpec](#schemajobspec)
- [ServiceOverrides](#serviceoverrides)
- [ServiceSpec](#servicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `spec` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Spec is a partial PodSpec (strategic-merge patch) merged onto the<br />generated pod template. It is stored as an opaque object to keep the<br />CRD schema small. |  | Optional: \{\} <br /> |


#### ProxyACL



ProxyACL restricts what the proxy relays.



_Appears in:_
- [TemporalClusterProxySpec](#temporalclusterproxyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `allowedNamespaces` _string array_ |  |  | Optional: \{\} <br /> |
| `allowedAdminMethods` _string array_ | AllowedAdminMethods defaults to the standard replication allowlist when empty. |  | Optional: \{\} <br /> |


#### ProxyFailoverVersionIncrement



ProxyFailoverVersionIncrement translates failover-version increments across the link.



_Appears in:_
- [TemporalClusterProxySpec](#temporalclusterproxyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `local` _integer_ |  |  |  |
| `remote` _integer_ |  |  |  |


#### ProxyFieldMapping



ProxyFieldMapping maps a local search-attribute field name to a remote one.



_Appears in:_
- [ProxySearchAttributeMapping](#proxysearchattributemapping)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `localFieldName` _string_ |  |  |  |
| `remoteFieldName` _string_ |  |  |  |


#### ProxyMux



ProxyMux configures the s2s-proxy mux transport for one link.



_Appears in:_
- [TemporalClusterProxySpec](#temporalclusterproxyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `role` _string_ | Role selects whether this proxy opens a port (server) or dials out (client). |  | Enum: [server client] <br /> |
| `server` _[ProxyMuxServer](#proxymuxserver)_ | Server configures the listening side. Required when role=server. |  | Optional: \{\} <br /> |
| `client` _[ProxyMuxClient](#proxymuxclient)_ | Client configures the dialing side. Required when role=client. |  | Optional: \{\} <br /> |
| `muxCount` _integer_ | MuxCount is the number of multiplexed sessions. Defaults to the upstream default. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `tls` _[ProxyMuxTLS](#proxymuxtls)_ | TLS configures the mux mTLS material. |  |  |


#### ProxyMuxClient



ProxyMuxClient configures a mux-client (dialing) proxy.



_Appears in:_
- [ProxyMux](#proxymux)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serverAddress` _string_ | ServerAddress is the reachable host:port of the remote mux-server. |  |  |


#### ProxyMuxServer



ProxyMuxServer configures a mux-server (listening) proxy.



_Appears in:_
- [ProxyMux](#proxymux)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `listenPort` _integer_ | ListenPort is the port the mux listens on. |  | Maximum: 65535 <br />Minimum: 1 <br /> |
| `exposure` _[ServiceExposureSpec](#serviceexposurespec)_ | Exposure controls how the mux port is exposed (ClusterIP/NodePort/LoadBalancer). |  | Optional: \{\} <br /> |


#### ProxyMuxTLS



ProxyMuxTLS configures the mux mTLS material for one side.



_Appears in:_
- [ProxyMux](#proxymux)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `provider` _string_ | Provider selects how this side's mux certificate is sourced. | cert-manager | Enum: [cert-manager secret] <br />Optional: \{\} <br /> |
| `issuerRef` _[IssuerReference](#issuerreference)_ | IssuerRef mints this side's mux certificate. Required when provider=cert-manager. |  | Optional: \{\} <br /> |
| `secretRef` _[SecretReference](#secretreference)_ | SecretRef supplies BYO cert/key/CA. Required when provider=secret. |  | Optional: \{\} <br /> |
| `peerCARef` _[SecretReference](#secretreference)_ | PeerCARef supplies the remote side's CA to trust. When unset the CA bundle<br />from this side's own material is used (shared-issuer case). |  | Optional: \{\} <br /> |


#### ProxyNamespaceMapping



ProxyNamespaceMapping maps a local namespace name to a remote one.



_Appears in:_
- [ProxyTranslation](#proxytranslation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `local` _string_ |  |  |  |
| `remote` _string_ |  |  |  |


#### ProxyPeer



ProxyPeer identifies the remote replication cluster reached over the mux.



_Appears in:_
- [TemporalClusterProxySpec](#temporalclusterproxyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the remote replication cluster name (== its currentClusterName). |  |  |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef optionally references an operator-managed remote TemporalCluster.<br />It is used only to reuse the peer's issuer CA when available. |  | Optional: \{\} <br /> |
| `enableConnection` _boolean_ | EnableConnection toggles replication without deleting the CR. | true | Optional: \{\} <br /> |


#### ProxySearchAttributeMapping



ProxySearchAttributeMapping maps search-attribute field names for a namespace.



_Appears in:_
- [ProxyTranslation](#proxytranslation)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ |  |  |  |
| `mappings` _[ProxyFieldMapping](#proxyfieldmapping) array_ |  |  |  |


#### ProxyTranslation



ProxyTranslation renames namespaces and search attributes in-flight.



_Appears in:_
- [TemporalClusterProxySpec](#temporalclusterproxyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespaces` _[ProxyNamespaceMapping](#proxynamespacemapping) array_ |  |  | Optional: \{\} <br /> |
| `searchAttributes` _[ProxySearchAttributeMapping](#proxysearchattributemapping) array_ |  |  | Optional: \{\} <br /> |


#### RetryPolicySpec



RetryPolicySpec is the retry policy for the started workflow.



_Appears in:_
- [StartWorkflowAction](#startworkflowaction)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `initialInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `backoffCoefficient` _string_ | BackoffCoefficient is a decimal string (e.g. "2.0") parsed to float64. |  | Optional: \{\} <br /> |
| `maximumInterval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `maximumAttempts` _integer_ |  |  | Optional: \{\} <br /> |
| `nonRetryableErrorTypes` _string array_ |  |  | Optional: \{\} <br /> |


#### SQLDatastoreSpec



SQLDatastoreSpec configures a SQL (Postgres/MySQL) datastore.



_Appears in:_
- [DatastoreSpec](#datastorespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pluginName` _string_ | PluginName selects the SQL driver. | postgres12 | Enum: [postgres12 postgres12_pgx mysql8] <br /> |
| `host` _string_ |  |  |  |
| `port` _integer_ |  | 5432 | Maximum: 65535 <br />Minimum: 1 <br /> |
| `database` _string_ |  |  |  |
| `user` _string_ |  |  |  |
| `passwordSecretRef` _[SecretKeyReference](#secretkeyreference)_ | PasswordSecretRef references a secret containing the password. Required<br />for password authentication. |  | Optional: \{\} <br /> |
| `passwordCommandSecretRef` _[SecretKeyReference](#secretkeyreference)_ | PasswordCommandSecretRef references a secret holding a command that emits<br />a short-lived credential (Temporal 1.31+ IAM auth). |  | Optional: \{\} <br /> |
| `connectAttributes` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `maxConns` _integer_ |  |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxIdleConns` _integer_ |  |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxConnLifetime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `tls` _[DatastoreTLSSpec](#datastoretlsspec)_ |  |  | Optional: \{\} <br /> |


#### ScheduleActionSpec



ScheduleActionSpec is the action taken when the schedule fires.



_Appears in:_
- [TemporalScheduleSpec](#temporalschedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `startWorkflow` _[StartWorkflowAction](#startworkflowaction)_ |  |  |  |


#### SchedulePoliciesSpec



SchedulePoliciesSpec tunes overlap/catchup behavior.



_Appears in:_
- [TemporalScheduleSpec](#temporalschedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `overlapPolicy` _string_ |  |  | Enum: [Skip BufferOne BufferAll CancelOther TerminateOther AllowAll] <br />Optional: \{\} <br /> |
| `catchupWindow` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `pauseOnFailure` _boolean_ |  |  | Optional: \{\} <br /> |
| `keepOriginalWorkflowID` _boolean_ |  |  | Optional: \{\} <br /> |


#### ScheduleSpec



ScheduleSpec is the set of times an action should occur at.



_Appears in:_
- [TemporalScheduleSpec](#temporalschedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `calendars` _string array_ | Calendars holds cron strings (5/6/7 field, or @daily etc). |  | Optional: \{\} <br /> |
| `intervals` _[IntervalSpec](#intervalspec) array_ | Intervals fire every Every (plus optional Offset/phase). |  | Optional: \{\} <br /> |
| `structuredCalendar` _[StructuredCalendarSpec](#structuredcalendarspec) array_ | StructuredCalendar gives field-level control without cron syntax. |  | Optional: \{\} <br /> |
| `excludeStructuredCalendar` _[StructuredCalendarSpec](#structuredcalendarspec) array_ | ExcludeStructuredCalendar subtracts matching times. |  | Optional: \{\} <br /> |
| `startTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | StartTime bounds the schedule start (inclusive). |  | Optional: \{\} <br /> |
| `endTime` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ | EndTime bounds the schedule end (inclusive). |  | Optional: \{\} <br /> |
| `jitter` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | Jitter randomizes each action time by 0..Jitter. |  | Optional: \{\} <br /> |
| `timezoneName` _string_ | TimezoneName interprets calendar specs (IANA name; defaults to UTC). |  | Optional: \{\} <br /> |


#### ScheduleStateSpec



ScheduleStateSpec controls pause and action-limit state.



_Appears in:_
- [TemporalScheduleSpec](#temporalschedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `paused` _boolean_ |  |  | Optional: \{\} <br /> |
| `notes` _string_ |  |  | Optional: \{\} <br /> |
| `limitedActions` _boolean_ |  |  | Optional: \{\} <br /> |
| `remainingActions` _integer_ |  |  | Optional: \{\} <br /> |


#### SchemaJobSpec



SchemaJobSpec customizes the schema management Jobs (setup-schema /
update-schema) the operator runs against SQL and Cassandra datastores.



_Appears in:_
- [PersistenceSpec](#persistencespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `podTemplate` _[PodTemplateOverride](#podtemplateoverride)_ | PodTemplate overrides metadata and the pod spec of the schema Job pods.<br />Use it to attach a ServiceAccount, pod labels (e.g. Azure Workload<br />Identity), and a token initContainer so the Job can authenticate with a<br />passwordCommand instead of a static password. |  | Optional: \{\} <br /> |


#### SchemaUpgradeRecord



SchemaUpgradeRecord records a single schema migration.



_Appears in:_
- [PersistenceStatus](#persistencestatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `store` _string_ |  |  |  |
| `fromVersion` _string_ |  |  |  |
| `toVersion` _string_ |  |  |  |
| `time` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ |  |  |  |


#### SecretKeyReference



SecretKeyReference references a single key within a Secret in the same namespace.



_Appears in:_
- [CassandraDatastoreSpec](#cassandradatastorespec)
- [DatastoreTLSSpec](#datastoretlsspec)
- [ElasticsearchDatastoreSpec](#elasticsearchdatastorespec)
- [SQLDatastoreSpec](#sqldatastorespec)
- [UIAuthSpec](#uiauthspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `key` _string_ |  | password | Optional: \{\} <br /> |


#### SecretReference



SecretReference points at a Secret in the same namespace holding TLS material
for connecting to an external Temporal peer. Keys default to the conventional
"ca.crt", "tls.crt", "tls.key" when the overrides are empty.



_Appears in:_
- [ClusterConnectionPeer](#clusterconnectionpeer)
- [ProxyMuxTLS](#proxymuxtls)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the Secret name. |  |  |
| `caKey` _string_ | CAKey is the Secret key holding the CA bundle. Defaults to "ca.crt". |  | Optional: \{\} <br /> |
| `certKey` _string_ | CertKey is the Secret key holding the client certificate. Defaults to "tls.crt". |  | Optional: \{\} <br /> |
| `keyKey` _string_ | KeyKey is the Secret key holding the client private key. Defaults to "tls.key". |  | Optional: \{\} <br /> |


#### ServiceExposureSpec



ServiceExposureSpec configures how a service is exposed.



_Appears in:_
- [ProxyMuxServer](#proxymuxserver)
- [ServiceSpec](#servicespec)
- [TemporalDevServerSpec](#temporaldevserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[ServiceType](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#servicetype-v1-core)_ |  | ClusterIP | Enum: [ClusterIP NodePort LoadBalancer] <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |


#### ServiceMonitorSpec



ServiceMonitorSpec configures a Prometheus Operator ServiceMonitor.



_Appears in:_
- [MetricsSpec](#metricsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | false |  |
| `interval` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `labels` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |


#### ServiceOverrides



ServiceOverrides are shared defaults applied across services.



_Appears in:_
- [ServicesSpec](#servicesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `podTemplate` _[PodTemplateOverride](#podtemplateoverride)_ |  |  | Optional: \{\} <br /> |


#### ServiceSpec



ServiceSpec configures a single Temporal service deployment.



_Appears in:_
- [ServicesSpec](#servicesspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ |  | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcerequirements-v1-core)_ |  |  | Optional: \{\} <br /> |
| `podTemplate` _[PodTemplateOverride](#podtemplateoverride)_ |  |  | Optional: \{\} <br /> |
| `service` _[ServiceExposureSpec](#serviceexposurespec)_ |  |  | Optional: \{\} <br /> |
| `nodeSelector` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `tolerations` _[Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#toleration-v1-core) array_ |  |  | Optional: \{\} <br /> |
| `affinity` _[Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#affinity-v1-core)_ |  |  | Optional: \{\} <br /> |
| `topologySpreadConstraints` _[TopologySpreadConstraint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#topologyspreadconstraint-v1-core) array_ |  |  | Optional: \{\} <br /> |


#### ServiceStatus



ServiceStatus reports the readiness of a single service.



_Appears in:_
- [TemporalClusterStatus](#temporalclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _integer_ |  |  | Optional: \{\} <br /> |
| `desired` _integer_ |  |  | Optional: \{\} <br /> |
| `version` _string_ |  |  | Optional: \{\} <br /> |


#### ServicesSpec



ServicesSpec configures each Temporal service plus shared overrides.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `frontend` _[ServiceSpec](#servicespec)_ |  |  | Optional: \{\} <br /> |
| `history` _[ServiceSpec](#servicespec)_ |  |  | Optional: \{\} <br /> |
| `matching` _[ServiceSpec](#servicespec)_ |  |  | Optional: \{\} <br /> |
| `worker` _[ServiceSpec](#servicespec)_ |  |  | Optional: \{\} <br /> |
| `internalFrontend` _[InternalFrontendSpec](#internalfrontendspec)_ |  |  | Optional: \{\} <br /> |
| `overrides` _[ServiceOverrides](#serviceoverrides)_ | Overrides are applied to every service unless overridden per-service. |  | Optional: \{\} <br /> |


#### StartWorkflowAction



StartWorkflowAction starts a workflow when the schedule fires.



_Appears in:_
- [ScheduleActionSpec](#scheduleactionspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `workflowType` _string_ |  |  |  |
| `taskQueue` _string_ |  |  |  |
| `workflowID` _string_ |  |  | Optional: \{\} <br /> |
| `args` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg) array_ | Args are JSON-serializable workflow inputs (one json/plain payload each). |  | Optional: \{\} <br /> |
| `workflowExecutionTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `workflowRunTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `workflowTaskTimeout` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ |  |  | Optional: \{\} <br /> |
| `workflowIDReusePolicy` _string_ |  |  | Enum: [AllowDuplicate AllowDuplicateFailedOnly RejectDuplicate TerminateIfRunning] <br />Optional: \{\} <br /> |
| `retryPolicy` _[RetryPolicySpec](#retrypolicyspec)_ |  |  | Optional: \{\} <br /> |
| `memo` _object (keys:string, values:[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg))_ |  |  | Optional: \{\} <br /> |
| `searchAttributes` _object (keys:string, values:[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg))_ |  |  | Optional: \{\} <br /> |


#### StructuredCalendarSpec



StructuredCalendarSpec describes calendar times as field ranges.



_Appears in:_
- [ScheduleSpec](#schedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `second` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `minute` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `hour` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `dayOfMonth` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `month` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `year` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `dayOfWeek` _[CalendarRange](#calendarrange) array_ |  |  | Optional: \{\} <br /> |
| `comment` _string_ |  |  | Optional: \{\} <br /> |


#### TemporalCluster



TemporalCluster is the Schema for the temporalclusters API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalCluster` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalClusterSpec](#temporalclusterspec)_ | spec defines the desired state of TemporalCluster |  | Required: \{\} <br /> |


#### TemporalClusterClient



TemporalClusterClient is the Schema for the temporalclusterclients API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalClusterClient` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalClusterClientSpec](#temporalclusterclientspec)_ | spec defines the desired state of TemporalClusterClient |  | Required: \{\} <br /> |


#### TemporalClusterClientSpec



TemporalClusterClientSpec defines the desired state of TemporalClusterClient.



_Appears in:_
- [TemporalClusterClient](#temporalclusterclient)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef references the cluster to generate client credentials for.<br />Client credentials are only available for mTLS-enabled TemporalClusters. |  |  |
| `secretName` _string_ | SecretName is the name of the Secret to write generated client credentials into.<br />Defaults to the resource name when empty. |  | Optional: \{\} <br /> |




#### TemporalClusterConnection



TemporalClusterConnection is the Schema for the temporalclusterconnections API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalClusterConnection` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalClusterConnectionSpec](#temporalclusterconnectionspec)_ |  |  | Required: \{\} <br /> |


#### TemporalClusterConnectionSpec



TemporalClusterConnectionSpec defines a multi-cluster replication group and
drives automatic remote-cluster connection registration between its peers.



_Appears in:_
- [TemporalClusterConnection](#temporalclusterconnection)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `peers` _[ClusterConnectionPeer](#clusterconnectionpeer) array_ | Peers participating in replication. At least two are required. Each peer's<br />Name must equal that cluster's clusterMetadata.currentClusterName. |  | MinItems: 2 <br /> |




#### TemporalClusterProxy



TemporalClusterProxy is the Schema for the temporalclusterproxies API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalClusterProxy` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalClusterProxySpec](#temporalclusterproxyspec)_ |  |  | Required: \{\} <br /> |


#### TemporalClusterProxySpec



TemporalClusterProxySpec describes one local cluster's s2s-proxy and its link
to one replication peer over an s2s-proxy mux connection.



_Appears in:_
- [TemporalClusterProxy](#temporalclusterproxy)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `localClusterRef` _[ClusterReference](#clusterreference)_ | LocalClusterRef references the local operator-managed TemporalCluster this<br />proxy fronts. Its frontend address and issuer CA are resolved automatically. |  |  |
| `localClusterName` _string_ | LocalClusterName overrides the replication-group name of the local cluster.<br />Defaults to the referenced cluster's clusterMetadata.currentClusterName. |  | Optional: \{\} <br /> |
| `peer` _[ProxyPeer](#proxypeer)_ | Peer is the remote replication cluster on the far side of the mux. |  |  |
| `mux` _[ProxyMux](#proxymux)_ | Mux configures the s2s-proxy multiplexed transport. |  |  |
| `translation` _[ProxyTranslation](#proxytranslation)_ | Translation optionally renames namespaces and search attributes in-flight. |  | Optional: \{\} <br /> |
| `failoverVersionIncrement` _[ProxyFailoverVersionIncrement](#proxyfailoverversionincrement)_ | FailoverVersionIncrement optionally translates failover-version increments. |  | Optional: \{\} <br /> |
| `acl` _[ProxyACL](#proxyacl)_ | ACL optionally restricts the admin methods and namespaces the proxy relays. |  | Optional: \{\} <br /> |
| `image` _string_ | Image overrides the pinned s2s-proxy image. |  | Optional: \{\} <br /> |




#### TemporalClusterSpec



TemporalClusterSpec defines the desired state of TemporalCluster.



_Appears in:_
- [TemporalCluster](#temporalcluster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `version` _string_ | Version is the Temporal server version, e.g. "1.31.1". |  | Pattern: `^\d+\.\d+\.\d+$` <br /> |
| `numHistoryShards` _integer_ | NumHistoryShards is the number of history shards. IMMUTABLE after creation.<br />Choose carefully: 512 small prod, 4096 large prod. | 512 | Maximum: 16384 <br />Minimum: 1 <br /> |
| `image` _string_ | Image is the Temporal server image. Default: temporalio/server:<Version>. |  | Optional: \{\} <br /> |
| `imagePullSecrets` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core) array_ | ImagePullSecrets references secrets for pulling the server image. |  | Optional: \{\} <br /> |
| `services` _[ServicesSpec](#servicesspec)_ | Services configures each Temporal service. |  | Optional: \{\} <br /> |
| `persistence` _[PersistenceSpec](#persistencespec)_ | Persistence configures the default and visibility datastores. Required. |  |  |
| `mtls` _[MTLSSpec](#mtlsspec)_ | MTLS configures mutual TLS (cert-manager-driven by default). |  | Optional: \{\} <br /> |
| `dynamicConfig` _[DynamicConfigSpec](#dynamicconfigspec)_ | DynamicConfig is a passthrough for Temporal's dynamic config. |  | Optional: \{\} <br /> |
| `ui` _[UISpec](#uispec)_ | UI configures temporal-ui as part of this cluster. |  | Optional: \{\} <br /> |
| `metrics` _[MetricsSpec](#metricsspec)_ | Metrics configures Prometheus integration. |  | Optional: \{\} <br /> |
| `archival` _[ArchivalSpec](#archivalspec)_ | Archival configures cluster-wide archival enablement. |  | Optional: \{\} <br /> |
| `authorization` _[AuthorizationSpec](#authorizationspec)_ | Authorization configures the authorizer and claim mapper. |  | Optional: \{\} <br /> |
| `clusterMetadata` _[ClusterMetadataSpec](#clustermetadataspec)_ | ClusterMetadata configures multi-cluster replication. |  | Optional: \{\} <br /> |
| `preventDeletion` _boolean_ | PreventDeletion, when true, blocks deletion of the cluster via the<br />validating webhook as a safety measure. |  | Optional: \{\} <br /> |




#### TemporalDevServer



TemporalDevServer is the Schema for the temporaldevservers API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalDevServer` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalDevServerSpec](#temporaldevserverspec)_ | spec defines the desired state of TemporalDevServer |  | Required: \{\} <br /> |


#### TemporalDevServerSpec



TemporalDevServerSpec defines the desired state of TemporalDevServer.

A TemporalDevServer runs a single-pod, disposable `temporal server start-dev`
instance backed by SQLite. It is NOT for production use.



_Appears in:_
- [TemporalDevServer](#temporaldevserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `version` _string_ | Version is the Temporal server version to run, e.g. "1.31.1". The operator<br />maps it to the matching temporalio/temporal CLI image via the supported<br />version matrix. When empty, the latest supported server version is used.<br />Use Image to pin a specific CLI image directly. |  | Optional: \{\} <br /> |
| `image` _string_ | Image overrides the full image reference. Default<br />temporalio/temporal:<Version>. |  | Optional: \{\} <br /> |
| `imagePullSecrets` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core) array_ | ImagePullSecrets references secrets for pulling the image. |  | Optional: \{\} <br /> |
| `namespaces` _string array_ | Namespaces are extra Temporal namespaces created at startup, in addition<br />to the always-present "default" namespace. These are created once at boot<br />with no drift management; use TemporalNamespace CRs for managed namespaces. |  | Optional: \{\} <br /> |
| `ui` _[DevServerUISpec](#devserveruispec)_ | UI controls the bundled Temporal Web UI (port 8233). |  | Optional: \{\} <br /> |
| `storage` _[DevServerStorageSpec](#devserverstoragespec)_ | Storage selects ephemeral (default) or PVC-backed SQLite storage. |  | Optional: \{\} <br /> |
| `service` _[ServiceExposureSpec](#serviceexposurespec)_ | Service configures how the frontend/UI Service is exposed. |  | Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#resourcerequirements-v1-core)_ | Resources sets the dev server container resource requirements. |  | Optional: \{\} <br /> |
| `nodeSelector` _object (keys:string, values:string)_ | NodeSelector constrains the dev server pod to matching nodes. |  | Optional: \{\} <br /> |
| `tolerations` _[Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#toleration-v1-core) array_ | Tolerations applied to the dev server pod. |  | Optional: \{\} <br /> |
| `affinity` _[Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#affinity-v1-core)_ | Affinity applied to the dev server pod. |  | Optional: \{\} <br /> |




#### TemporalNamespace



TemporalNamespace is the Schema for the temporalnamespaces API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalNamespace` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalNamespaceSpec](#temporalnamespacespec)_ | spec defines the desired state of TemporalNamespace |  | Required: \{\} <br /> |


#### TemporalNamespaceSpec



TemporalNamespaceSpec defines the desired state of TemporalNamespace.



_Appears in:_
- [TemporalNamespace](#temporalnamespace)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef references the TemporalCluster or TemporalDevServer that owns<br />this namespace. |  |  |
| `retentionPeriod` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RetentionPeriod is how long closed workflows are retained. | 72h | Optional: \{\} <br /> |
| `description` _string_ | Description is a human-friendly description of the namespace. |  | Optional: \{\} <br /> |
| `ownerEmail` _string_ | OwnerEmail is the owner contact for the namespace. |  | Optional: \{\} <br /> |
| `allowDeletion` _boolean_ | AllowDeletion permits the operator to delete the Temporal namespace when<br />the CR is deleted. When false, the namespace is left in place. |  | Optional: \{\} <br /> |
| `driftDetection` _string_ | DriftDetection controls whether the operator reconciles drift between the<br />spec and the live namespace. | reconcile | Enum: [reconcile ignore] <br />Optional: \{\} <br /> |
| `isGlobal` _boolean_ | IsGlobal marks the namespace as global for multi-cluster replication. |  | Optional: \{\} <br /> |
| `clusters` _string array_ | Clusters lists the cluster names this namespace is replicated to. Only<br />meaningful when IsGlobal is true. |  | Optional: \{\} <br /> |
| `activeCluster` _string_ | ActiveCluster is the authoritative cluster for this namespace. Changing it<br />triggers an operator-executed failover. Only meaningful when IsGlobal. |  | Optional: \{\} <br /> |




#### TemporalSchedule



TemporalSchedule is the Schema for the temporalschedules API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalSchedule` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalScheduleSpec](#temporalschedulespec)_ |  |  | Required: \{\} <br /> |


#### TemporalScheduleSpec



TemporalScheduleSpec defines the desired state of TemporalSchedule.



_Appears in:_
- [TemporalSchedule](#temporalschedule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef references the TemporalCluster or TemporalDevServer that hosts this schedule. |  |  |
| `namespace` _string_ | Namespace is the Temporal namespace the schedule lives in. |  |  |
| `scheduleID` _string_ | ScheduleID is the Temporal schedule ID. Defaults to metadata.name.<br />Immutable once set. |  | Optional: \{\} <br /> |
| `allowDeletion` _boolean_ | AllowDeletion permits the operator to delete the Temporal schedule when<br />the CR is deleted. When false, the schedule is left in place. |  | Optional: \{\} <br /> |
| `schedule` _[ScheduleSpec](#schedulespec)_ | Schedule describes when the action fires. |  |  |
| `action` _[ScheduleActionSpec](#scheduleactionspec)_ | Action describes what to do when the schedule fires. |  |  |
| `policies` _[SchedulePoliciesSpec](#schedulepoliciesspec)_ | Policies tunes overlap/catchup/pause-on-failure behavior. |  | Optional: \{\} <br /> |
| `state` _[ScheduleStateSpec](#schedulestatespec)_ | State controls pause and action-limit state. |  | Optional: \{\} <br /> |




#### TemporalSearchAttribute



TemporalSearchAttribute is the Schema for the temporalsearchattributes API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `temporal.bmor10.com/v1alpha1` | | |
| `kind` _string_ | `TemporalSearchAttribute` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[TemporalSearchAttributeSpec](#temporalsearchattributespec)_ | spec defines the desired state of TemporalSearchAttribute |  | Required: \{\} <br /> |


#### TemporalSearchAttributeSpec



TemporalSearchAttributeSpec defines the desired state of TemporalSearchAttribute.



_Appears in:_
- [TemporalSearchAttribute](#temporalsearchattribute)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clusterRef` _[ClusterReference](#clusterreference)_ | ClusterRef references the TemporalCluster or TemporalDevServer this search attribute belongs to. |  |  |
| `namespace` _string_ | Namespace is the Temporal namespace to register the attribute in. |  |  |
| `name` _string_ | Name is the search attribute name. |  |  |
| `type` _string_ | Type is the search attribute type. Immutable once created. |  | Enum: [Keyword Text Int Double Bool Datetime KeywordList] <br /> |
| `allowDeletion` _boolean_ | AllowDeletion permits the operator to remove the search attribute from the<br />namespace when the CR is deleted. |  | Optional: \{\} <br /> |




#### UIAuthSpec



UIAuthSpec configures temporal-ui OIDC authentication.



_Appears in:_
- [UISpec](#uispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | false |  |
| `entra` _[EntraUIAuthSpec](#entrauiauthspec)_ | Entra derives ProviderURL from a Microsoft Entra tenant ID. |  | Optional: \{\} <br /> |
| `providerURL` _string_ | ProviderURL is the OIDC issuer URL (set directly or via Entra). |  | Optional: \{\} <br /> |
| `clientID` _string_ |  |  | Optional: \{\} <br /> |
| `clientSecretRef` _[SecretKeyReference](#secretkeyreference)_ | ClientSecretRef references a Secret key holding the OIDC client secret. |  | Optional: \{\} <br /> |
| `scopes` _string array_ | Scopes default to ["openid", "profile", "email"]. |  | Optional: \{\} <br /> |
| `callbackURL` _string_ |  |  | Optional: \{\} <br /> |
| `extraEnv` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | ExtraEnv is a passthrough of additional temporal-ui auth env vars<br />(map of string to string). |  | Optional: \{\} <br /> |


#### UICodecServerSpec



UICodecServerSpec configures the temporal-ui codec server.



_Appears in:_
- [UISpec](#uispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ |  |  |  |
| `passAccessToken` _boolean_ |  |  | Optional: \{\} <br /> |
| `includeCredentials` _boolean_ |  |  | Optional: \{\} <br /> |


#### UIIngressSpec



UIIngressSpec configures ingress for temporal-ui.



_Appears in:_
- [UISpec](#uispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | false |  |
| `ingressClassName` _string_ |  |  | Optional: \{\} <br /> |
| `host` _string_ |  |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `tlsSecretName` _string_ |  |  | Optional: \{\} <br /> |


#### UISpec



UISpec configures temporal-ui.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  | false |  |
| `version` _string_ |  |  | Optional: \{\} <br /> |
| `replicas` _integer_ |  | 1 | Minimum: 1 <br />Optional: \{\} <br /> |
| `ingress` _[UIIngressSpec](#uiingressspec)_ |  |  | Optional: \{\} <br /> |
| `auth` _[UIAuthSpec](#uiauthspec)_ | Auth configures temporal-ui authentication (OIDC, e.g. Microsoft Entra). |  | Optional: \{\} <br /> |
| `codecServer` _[UICodecServerSpec](#uicodecserverspec)_ |  |  | Optional: \{\} <br /> |


#### UpgradeStatus



UpgradeStatus reports the state of an in-progress version upgrade.



_Appears in:_
- [TemporalClusterStatus](#temporalclusterstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `fromVersion` _string_ |  |  | Optional: \{\} <br /> |
| `toVersion` _string_ |  |  | Optional: \{\} <br /> |
| `phase` _string_ |  |  | Optional: \{\} <br /> |
| `rollbackable` _boolean_ | Rollbackable is true until schema migration begins, after which a<br />rollback is no longer safe. |  | Optional: \{\} <br /> |
| `startedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ |  |  | Optional: \{\} <br /> |


