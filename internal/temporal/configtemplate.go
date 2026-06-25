/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package temporal

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

//go:embed templates/config_template.yaml
var configTemplateFS embed.FS

// Default gRPC, membership, and HTTP ports for each Temporal service.
const (
	defaultFrontendGRPCPort       = 7233
	defaultFrontendMembershipPort = 6933
	defaultFrontendHTTPPort       = 7243

	defaultInternalFrontendGRPCPort       = 7236
	defaultInternalFrontendMembershipPort = 6936
	defaultInternalFrontendHTTPPort       = 7246

	defaultMatchingGRPCPort       = 7235
	defaultMatchingMembershipPort = 6935

	defaultHistoryGRPCPort       = 7234
	defaultHistoryMembershipPort = 6934

	defaultWorkerGRPCPort       = 7239
	defaultWorkerMembershipPort = 6939
)

// Conventional in-pod mount paths.
const (
	defaultDynamicConfigPath = "/etc/temporal/dynamicconfig/dynamic_config.yaml"

	internodeCertDir  = "/etc/temporal/certs/internode"
	frontendCertDir   = "/etc/temporal/certs/frontend"
	persistenceTLSdir = "/etc/temporal/certs/persistence"
)

// ServicePort holds the ports for a single Temporal service.
type ServicePort struct {
	GRPCPort       int32
	MembershipPort int32
	HTTPPort       int32
}

// DatastoreTLS holds TLS file paths for a datastore connection.
type DatastoreTLS struct {
	Enabled                bool
	CAFile                 string
	CertFile               string
	KeyFile                string
	EnableHostVerification bool
	ServerName             string
}

// DatastoreConfig is a fully-resolved datastore configuration.
type DatastoreConfig struct {
	// Kind is "sql", "cassandra", or "elasticsearch".
	Kind string

	// SQL fields.
	PluginName      string
	DatabaseName    string
	ConnectAddr     string
	User            string
	Password        string
	PasswordCommand string
	MaxConns        int32
	MaxIdleConns    int32
	MaxConnLifetime string

	// Cassandra fields.
	Hosts      string
	Keyspace   string
	Port       int32
	Datacenter string

	// Elasticsearch fields.
	ESVersion       string
	URLScheme       string
	URLHost         string
	VisibilityIndex string

	TLS *DatastoreTLS
}

// MTLSConfig holds resolved mTLS file paths and settings.
type MTLSConfig struct {
	Enabled             bool
	RefreshInterval     string
	RequireClientAuth   bool
	InternodeServerCert string
	InternodeServerKey  string
	InternodeClientCA   string
	InternodeServerName string
	FrontendServerCert  string
	FrontendServerKey   string
	FrontendServerName  string
	SystemWorkerCert    string
	SystemWorkerKey     string
	SystemWorkerCA      string
	SystemWorkerName    string
}

// MetricsConfig holds resolved Prometheus settings.
type MetricsConfig struct {
	Enabled       bool
	ListenAddress string
}

// AuthConfig holds resolved authorization settings.
type AuthConfig struct {
	Authorizer           string
	EmitAuthorizer       bool
	ClaimMapper          string
	PermissionsClaimName string
	KeySourceURIs        []string
	RefreshInterval      string
	ExtraConfig          map[string]interface{}
}

// ArchivalConfig holds resolved archival settings.
type ArchivalConfig struct {
	HistoryState    string
	VisibilityState string
}

// ConfigData is the fully-resolved input to the config template.
type ConfigData struct {
	Version                  string
	LogLevel                 string
	NumHistoryShards         int32
	DefaultStoreName         string
	VisibilityStoreName      string
	DefaultStore             DatastoreConfig
	VisibilityStore          DatastoreConfig
	BindOnIP                 string
	BroadcastAddress         string
	MTLS                     MTLSConfig
	Metrics                  MetricsConfig
	Authorization            *AuthConfig
	Services                 map[string]ServicePort
	UseInternalFrontend      bool
	EnableGlobalNamespace    bool
	CurrentClusterName       string
	MasterClusterName        string
	FailoverVersionIncrement int
	InitialFailoverVersion   int
	DynamicConfigPath        string
	Archival                 *ArchivalConfig
	PublicClient             string
}

// BuildOptions carries runtime-resolved values that are not derivable from the
// CR alone (resolved secret values and pod-network details).
type BuildOptions struct {
	// BindOnIP is the address services bind to. Defaults to "0.0.0.0".
	BindOnIP string
	// BroadcastAddress is the membership broadcast address (typically the pod IP).
	BroadcastAddress string
	// DynamicConfigPath overrides the default dynamic config file path.
	DynamicConfigPath string
	// DefaultStorePassword / VisibilityStorePassword are the resolved passwords.
	DefaultStorePassword    string
	VisibilityStorePassword string
	// DefaultStorePasswordCommand / VisibilityStorePasswordCommand are used for
	// Temporal 1.31+ IAM auth instead of a static password.
	DefaultStorePasswordCommand    string
	VisibilityStorePasswordCommand string
	// PublicClientHostPort overrides the publicClient host:port.
	PublicClientHostPort string
}

func defaultServices() map[string]ServicePort {
	return map[string]ServicePort{
		"frontend":         {GRPCPort: defaultFrontendGRPCPort, MembershipPort: defaultFrontendMembershipPort, HTTPPort: defaultFrontendHTTPPort},
		"internalFrontend": {GRPCPort: defaultInternalFrontendGRPCPort, MembershipPort: defaultInternalFrontendMembershipPort, HTTPPort: defaultInternalFrontendHTTPPort},
		"matching":         {GRPCPort: defaultMatchingGRPCPort, MembershipPort: defaultMatchingMembershipPort},
		"history":          {GRPCPort: defaultHistoryGRPCPort, MembershipPort: defaultHistoryMembershipPort},
		"worker":           {GRPCPort: defaultWorkerGRPCPort, MembershipPort: defaultWorkerMembershipPort},
	}
}

// DefaultServicePorts returns the default ports for each Temporal service keyed
// by the service's component name (frontend, history, matching, worker,
// internal-frontend).
func DefaultServicePorts() map[string]ServicePort {
	return map[string]ServicePort{
		"frontend":          {GRPCPort: defaultFrontendGRPCPort, MembershipPort: defaultFrontendMembershipPort, HTTPPort: defaultFrontendHTTPPort},
		"internal-frontend": {GRPCPort: defaultInternalFrontendGRPCPort, MembershipPort: defaultInternalFrontendMembershipPort, HTTPPort: defaultInternalFrontendHTTPPort},
		"matching":          {GRPCPort: defaultMatchingGRPCPort, MembershipPort: defaultMatchingMembershipPort},
		"history":           {GRPCPort: defaultHistoryGRPCPort, MembershipPort: defaultHistoryMembershipPort},
		"worker":            {GRPCPort: defaultWorkerGRPCPort, MembershipPort: defaultWorkerMembershipPort},
	}
}

// broadcastAddressOrDefault returns addr if non-empty, otherwise the Temporal
// env-var placeholder "${POD_IP}". The Temporal config loader evaluates
// ${VAR} references at server startup, so the actual pod IP is injected at
// runtime via the Kubernetes downward API.
func broadcastAddressOrDefault(addr string) string {
	if addr != "" {
		return addr
	}
	return "${POD_IP}"
}

func sqlConnLifetime(spec *temporalv1alpha1.SQLDatastoreSpec) string {
	if spec.MaxConnLifetime != nil {
		return spec.MaxConnLifetime.Duration.String()
	}
	return "1h"
}

func orDefault(v, def int32) int32 {
	if v == 0 {
		return def
	}
	return v
}

func datastoreTLS(spec *temporalv1alpha1.DatastoreTLSSpec, dir string) *DatastoreTLS {
	if spec == nil || !spec.Enabled {
		return nil
	}
	return &DatastoreTLS{
		Enabled:                true,
		CAFile:                 dir + "/ca.crt",
		CertFile:               dir + "/tls.crt",
		KeyFile:                dir + "/tls.key",
		EnableHostVerification: spec.EnableHostVerification,
		ServerName:             spec.ServerName,
	}
}

func buildDatastore(store temporalv1alpha1.DatastoreSpec, storeName, password, passwordCommand string) (DatastoreConfig, string, error) {
	switch {
	case store.SQL != nil:
		s := store.SQL
		return DatastoreConfig{
			Kind:            "sql",
			PluginName:      s.PluginName,
			DatabaseName:    s.Database,
			ConnectAddr:     fmt.Sprintf("%s:%d", s.Host, s.Port),
			User:            s.User,
			Password:        password,
			PasswordCommand: passwordCommand,
			MaxConns:        orDefault(s.MaxConns, 20),
			MaxIdleConns:    orDefault(s.MaxIdleConns, 20),
			MaxConnLifetime: sqlConnLifetime(s),
			TLS:             datastoreTLS(s.TLS, persistenceTLSdir+"/"+storeName),
		}, "sql", nil
	case store.Cassandra != nil:
		c := store.Cassandra
		hosts := ""
		for i, h := range c.Hosts {
			if i > 0 {
				hosts += ","
			}
			hosts += h
		}
		return DatastoreConfig{
			Kind:       "cassandra",
			Hosts:      hosts,
			Keyspace:   c.Keyspace,
			User:       c.User,
			Password:   password,
			Port:       c.Port,
			Datacenter: c.Datacenter,
			MaxConns:   20,
			TLS:        datastoreTLS(c.TLS, persistenceTLSdir+"/"+storeName),
		}, "cassandra", nil
	case store.Elasticsearch != nil:
		e := store.Elasticsearch
		scheme := "http"
		if e.TLS != nil && e.TLS.Enabled {
			scheme = "https"
		}
		index := "temporal_visibility_v1"
		if v, ok := e.Indices["visibility"]; ok {
			index = v
		}
		return DatastoreConfig{
			Kind:            "elasticsearch",
			ESVersion:       e.Version,
			URLScheme:       scheme,
			URLHost:         e.URL,
			User:            e.Username,
			Password:        password,
			VisibilityIndex: index,
		}, "elasticsearch", nil
	default:
		return DatastoreConfig{}, "", fmt.Errorf("datastore %q has no backend configured", storeName)
	}
}

func buildMetrics(cluster *temporalv1alpha1.TemporalCluster) MetricsConfig {
	if cluster.Spec.Metrics != nil && !cluster.Spec.Metrics.Enabled {
		return MetricsConfig{}
	}
	port := int32(9090)
	if cluster.Spec.Metrics != nil && cluster.Spec.Metrics.Port != 0 {
		port = cluster.Spec.Metrics.Port
	}
	return MetricsConfig{Enabled: true, ListenAddress: fmt.Sprintf("0.0.0.0:%d", port)}
}

func buildMTLS(cluster *temporalv1alpha1.TemporalCluster) MTLSConfig {
	if cluster.Spec.MTLS == nil {
		return MTLSConfig{}
	}
	refresh := "720h"
	if cluster.Spec.MTLS.RefreshInterval != nil {
		refresh = cluster.Spec.MTLS.RefreshInterval.Duration.String()
	}
	internodeServerName := cluster.Name + "-internode"
	frontendServerName := fmt.Sprintf("%s-frontend.%s.svc.cluster.local", cluster.Name, cluster.Namespace)
	return MTLSConfig{
		Enabled:             true,
		RefreshInterval:     refresh,
		RequireClientAuth:   true,
		InternodeServerCert: internodeCertDir + "/tls.crt",
		InternodeServerKey:  internodeCertDir + "/tls.key",
		InternodeClientCA:   internodeCertDir + "/ca.crt",
		InternodeServerName: internodeServerName,
		FrontendServerCert:  frontendCertDir + "/tls.crt",
		FrontendServerKey:   frontendCertDir + "/tls.key",
		FrontendServerName:  frontendServerName,
		SystemWorkerCert:    internodeCertDir + "/tls.crt",
		SystemWorkerKey:     internodeCertDir + "/tls.key",
		SystemWorkerCA:      internodeCertDir + "/ca.crt",
		SystemWorkerName:    frontendServerName,
	}
}

// buildAuthKeySourceURIs populates KeySourceURIs and PermissionsClaimName from
// the JWTKeyProvider and Entra blocks and returns whether any URIs were added.
func buildAuthKeySourceURIs(auth *temporalv1alpha1.AuthorizationSpec, cfg *AuthConfig) bool {
	if auth.JWTKeyProvider != nil {
		cfg.KeySourceURIs = append(cfg.KeySourceURIs, auth.JWTKeyProvider.KeySourceURIs...)
		if auth.JWTKeyProvider.RefreshInterval != nil {
			cfg.RefreshInterval = auth.JWTKeyProvider.RefreshInterval.Duration.String()
		}
	}
	if auth.Entra != nil {
		cfg.KeySourceURIs = append(cfg.KeySourceURIs,
			fmt.Sprintf("https://login.microsoftonline.com/%s/discovery/v2.0/keys", auth.Entra.TenantID))
		if cfg.PermissionsClaimName == "" {
			cfg.PermissionsClaimName = "roles"
		}
	}
	return len(cfg.KeySourceURIs) > 0
}

// applyJWTDefaults fills in default authorizer, claimMapper, and
// permissionsClaimName when JWT is configured and the user hasn't set them.
func applyJWTDefaults(auth *temporalv1alpha1.AuthorizationSpec, cfg *AuthConfig) {
	if auth.Authorizer == nil {
		cfg.Authorizer = "default"
		cfg.EmitAuthorizer = true
	}
	if cfg.ClaimMapper == "" {
		cfg.ClaimMapper = "default"
	}
	if cfg.PermissionsClaimName == "" {
		cfg.PermissionsClaimName = "permissions"
	}
}

// applyAuthConfigPassthrough unmarshals auth.Config and suppresses any modeled
// fields that are also present in the passthrough map to prevent duplicate keys.
func applyAuthConfigPassthrough(auth *temporalv1alpha1.AuthorizationSpec, cfg *AuthConfig) error {
	if auth.Config == nil || len(auth.Config.Raw) == 0 {
		return nil
	}
	extra := map[string]interface{}{}
	if err := yaml.Unmarshal(auth.Config.Raw, &extra); err != nil {
		return fmt.Errorf("authorization.config: %w", err)
	}
	cfg.ExtraConfig = extra
	// Suppress modeled fields that ExtraConfig also defines so the
	// passthrough wins and no duplicate YAML keys are emitted.
	if _, ok := extra["permissionsClaimName"]; ok {
		cfg.PermissionsClaimName = ""
	}
	if _, ok := extra["authorizer"]; ok {
		cfg.Authorizer = ""
		cfg.EmitAuthorizer = false
	}
	if _, ok := extra["claimMapper"]; ok {
		cfg.ClaimMapper = ""
	}
	if _, ok := extra["jwtKeyProvider"]; ok {
		cfg.KeySourceURIs = nil
		cfg.RefreshInterval = ""
	}
	return nil
}

func buildAuth(cluster *temporalv1alpha1.TemporalCluster) (*AuthConfig, error) {
	auth := cluster.Spec.Authorization
	if auth == nil {
		return nil, nil
	}

	cfg := &AuthConfig{
		ClaimMapper:          auth.ClaimMapper,
		PermissionsClaimName: auth.PermissionsClaimName,
	}

	// Honor an explicitly-set Authorizer (including "").
	if auth.Authorizer != nil {
		cfg.Authorizer = *auth.Authorizer
		cfg.EmitAuthorizer = true
	}

	jwtConfigured := buildAuthKeySourceURIs(auth, cfg)
	if jwtConfigured {
		applyJWTDefaults(auth, cfg)
	}

	if err := applyAuthConfigPassthrough(auth, cfg); err != nil {
		return nil, err
	}

	if !cfg.EmitAuthorizer && cfg.ClaimMapper == "" && !jwtConfigured && cfg.ExtraConfig == nil {
		return nil, nil
	}
	return cfg, nil
}

func applyClusterMetadata(data *ConfigData, cm *temporalv1alpha1.ClusterMetadataSpec) {
	if cm == nil {
		return
	}
	data.EnableGlobalNamespace = cm.EnableGlobalNamespace
	if cm.FailoverVersionIncrement != nil {
		data.FailoverVersionIncrement = int(*cm.FailoverVersionIncrement)
	}
	if cm.CurrentClusterName != "" {
		data.CurrentClusterName = cm.CurrentClusterName
	}
	if cm.MasterClusterName != "" {
		data.MasterClusterName = cm.MasterClusterName
	}
	if cm.InitialFailoverVersion != nil {
		data.InitialFailoverVersion = int(*cm.InitialFailoverVersion)
	}
}

// BuildConfigData resolves a TemporalCluster CR plus runtime options into a
// ConfigData ready for rendering.
func BuildConfigData(cluster *temporalv1alpha1.TemporalCluster, opts BuildOptions) (*ConfigData, error) {
	if cluster == nil {
		return nil, fmt.Errorf("cluster must not be nil")
	}

	bindOnIP := opts.BindOnIP
	if bindOnIP == "" {
		bindOnIP = "0.0.0.0"
	}
	dynamicConfigPath := opts.DynamicConfigPath
	if dynamicConfigPath == "" {
		dynamicConfigPath = defaultDynamicConfigPath
	}

	defaultStore, _, err := buildDatastore(cluster.Spec.Persistence.DefaultStore, "default", opts.DefaultStorePassword, opts.DefaultStorePasswordCommand)
	if err != nil {
		return nil, fmt.Errorf("default store: %w", err)
	}
	visStore, visKind, err := buildDatastore(cluster.Spec.Persistence.VisibilityStore, "visibility", opts.VisibilityStorePassword, opts.VisibilityStorePasswordCommand)
	if err != nil {
		return nil, fmt.Errorf("visibility store: %w", err)
	}

	visStoreName := "visibility"
	if visKind == "elasticsearch" {
		visStoreName = "es-visibility"
	}

	useInternalFrontend := cluster.Spec.Services.InternalFrontend != nil && cluster.Spec.Services.InternalFrontend.Enabled

	data := &ConfigData{
		Version:                  cluster.Spec.Version,
		LogLevel:                 "info",
		NumHistoryShards:         cluster.Spec.NumHistoryShards,
		DefaultStoreName:         "default",
		VisibilityStoreName:      visStoreName,
		DefaultStore:             defaultStore,
		VisibilityStore:          visStore,
		BindOnIP:                 bindOnIP,
		BroadcastAddress:         broadcastAddressOrDefault(opts.BroadcastAddress),
		Services:                 defaultServices(),
		CurrentClusterName:       "active",
		MasterClusterName:        "active",
		FailoverVersionIncrement: 10,
		InitialFailoverVersion:   1,
		DynamicConfigPath:        dynamicConfigPath,
		UseInternalFrontend:      useInternalFrontend,
		Metrics:                  buildMetrics(cluster),
		MTLS:                     buildMTLS(cluster),
	}

	auth, err := buildAuth(cluster)
	if err != nil {
		return nil, fmt.Errorf("authorization: %w", err)
	}
	data.Authorization = auth

	if cluster.Spec.Archival != nil {
		data.Archival = &ArchivalConfig{HistoryState: "enabled", VisibilityState: "enabled"}
	}

	applyClusterMetadata(data, cluster.Spec.ClusterMetadata)

	// publicClient is needed when there is no internal frontend.
	if !data.UseInternalFrontend {
		hostPort := opts.PublicClientHostPort
		if hostPort == "" {
			hostPort = fmt.Sprintf("127.0.0.1:%d", defaultFrontendGRPCPort)
		}
		data.PublicClient = hostPort
	}

	return data, nil
}

// includeFunc returns a template helper that renders a named associated template
// to a string, enabling output to be piped into indent.
func includeFunc(t *template.Template) func(string, interface{}) (string, error) {
	return func(name string, data interface{}) (string, error) {
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
}

// RenderConfig renders the resolved ConfigData into a Temporal server config YAML.
func RenderConfig(data *ConfigData) (string, error) {
	funcs := sprig.TxtFuncMap()
	t := template.New("config_template.yaml").Funcs(funcs)
	funcs["include"] = includeFunc(t)
	funcs["toYaml"] = func(v interface{}) (string, error) {
		out, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(out), "\n"), nil
	}
	t = t.Funcs(funcs)

	parsed, err := t.ParseFS(configTemplateFS, "templates/config_template.yaml")
	if err != nil {
		return "", fmt.Errorf("parsing config template: %w", err)
	}
	var buf bytes.Buffer
	if err := parsed.ExecuteTemplate(&buf, "config_template.yaml", data); err != nil {
		return "", fmt.Errorf("rendering config template: %w", err)
	}
	return buf.String(), nil
}

// RenderClusterConfig is a convenience that builds and renders in one step.
func RenderClusterConfig(cluster *temporalv1alpha1.TemporalCluster, opts BuildOptions) (string, error) {
	data, err := BuildConfigData(cluster, opts)
	if err != nil {
		return "", err
	}
	return RenderConfig(data)
}
