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
- [TemporalNamespace](#temporalnamespace)
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



AuthorizationSpec configures the authorizer and claim mapper.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `authorizer` _string_ |  |  | Optional: \{\} <br /> |
| `claimMapper` _string_ |  |  | Optional: \{\} <br /> |
| `config` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Config is a passthrough for authorization provider configuration. |  | Optional: \{\} <br /> |


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


#### ClusterMetadataSpec



ClusterMetadataSpec is a passthrough for multi-cluster metadata.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `raw` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ |  |  | Optional: \{\} <br /> |


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


#### IssuerReference



IssuerReference references a cert-manager Issuer or ClusterIssuer.



_Appears in:_
- [MTLSSpec](#mtlsspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `kind` _string_ |  | Issuer | Enum: [Issuer ClusterIssuer] <br />Optional: \{\} <br /> |
| `group` _string_ |  | cert-manager.io | Optional: \{\} <br /> |


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


#### PersistenceSpec



PersistenceSpec configures the default and visibility datastores.



_Appears in:_
- [TemporalClusterSpec](#temporalclusterspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `defaultStore` _[DatastoreSpec](#datastorespec)_ | DefaultStore holds workflow execution state. Exactly one of sql or<br />cassandra must be set. |  |  |
| `visibilityStore` _[DatastoreSpec](#datastorespec)_ | VisibilityStore holds visibility records. One of sql, cassandra, or<br />elasticsearch must be set. |  |  |


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
- [ServiceOverrides](#serviceoverrides)
- [ServiceSpec](#servicespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ |  |  | Optional: \{\} <br /> |
| `spec` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Spec is a partial PodSpec (strategic-merge patch) merged onto the<br />generated pod template. It is stored as an opaque object to keep the<br />CRD schema small. |  | Optional: \{\} <br /> |


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

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  |  |
| `key` _string_ |  | password | Optional: \{\} <br /> |


#### ServiceExposureSpec



ServiceExposureSpec configures how a service is exposed.



_Appears in:_
- [ServiceSpec](#servicespec)

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
| `clusterRef` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | ClusterRef references the TemporalCluster to generate client credentials for. |  |  |
| `secretName` _string_ | SecretName is the name of the Secret to write generated client credentials into.<br />Defaults to the resource name when empty. |  | Optional: \{\} <br /> |




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
| `clusterMetadata` _[ClusterMetadataSpec](#clustermetadataspec)_ | ClusterMetadata is a passthrough for multi-cluster setup. |  | Optional: \{\} <br /> |
| `preventDeletion` _boolean_ | PreventDeletion, when true, blocks deletion of the cluster via the<br />validating webhook as a safety measure. |  | Optional: \{\} <br /> |




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
| `clusterRef` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | ClusterRef references the TemporalCluster that owns this namespace. |  |  |
| `retentionPeriod` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#duration-v1-meta)_ | RetentionPeriod is how long closed workflows are retained. | 72h | Optional: \{\} <br /> |
| `description` _string_ | Description is a human-friendly description of the namespace. |  | Optional: \{\} <br /> |
| `ownerEmail` _string_ | OwnerEmail is the owner contact for the namespace. |  | Optional: \{\} <br /> |




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
| `clusterRef` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#localobjectreference-v1-core)_ | ClusterRef references the TemporalCluster this search attribute belongs to. |  |  |
| `namespace` _string_ | Namespace is the Temporal namespace to register the attribute in. |  |  |
| `name` _string_ | Name is the search attribute name. |  |  |
| `type` _string_ | Type is the search attribute type. Immutable once created. |  | Enum: [Keyword Text Int Double Bool Datetime KeywordList] <br /> |




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
| `auth` _[RawExtension](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#rawextension-runtime-pkg)_ | Auth is a passthrough for temporal-ui authentication config. |  | Optional: \{\} <br /> |
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
| `startedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.34/#time-v1-meta)_ |  |  | Optional: \{\} <br /> |


