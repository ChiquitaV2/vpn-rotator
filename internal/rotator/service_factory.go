package rotator

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
)

// ServiceFactory creates and wires all service components with proper dependency injection
type ServiceFactory struct {
	config *config.Config
	logger *slog.Logger
}

// NewServiceFactory creates a new service factory
func NewServiceFactory(cfg *config.Config, logger *slog.Logger) *ServiceFactory {
	return &ServiceFactory{
		config: cfg,
		logger: logger,
	}
}

// ServiceComponents holds all the service components
type ServiceComponents struct {
	// Infrastructure layer
	Store            db.Store
	CloudProvisioner provisioner.CloudProvisioner
	NodeInteractor   nodeinteractor.NodeInteractor

	// Domain services
	NodeService node.NodeService
	PeerService peer.Service
	IPService   ip.Service

	// Application services
	VPNService   application.VPNService
	AdminService application.AdminService

	// Domain services (additional)
	PeerManagementService *node.PeerManagementService
	ProvisioningService   *node.ProvisioningService

	// Repositories
	NodeRepository   node.NodeRepository
	PeerRepository   peer.Repository
	SubnetRepository ip.Repository
}

// CreateComponents creates all service components with proper dependency injection
func (f *ServiceFactory) CreateComponents() (*ServiceComponents, error) {
	f.logger.Info("creating service components with layered architecture")

	components := &ServiceComponents{}

	// 1. Initialize database store (foundational dependency)
	if err := f.createDatabaseStore(components); err != nil {
		return nil, fmt.Errorf("failed to create database store: %w", err)
	}

	// 2. Initialize repositories (depend on store)
	if err := f.createRepositories(components); err != nil {
		return nil, fmt.Errorf("failed to create repositories: %w", err)
	}

	// 3. Initialize infrastructure services
	if err := f.createInfrastructureServices(components); err != nil {
		return nil, fmt.Errorf("failed to create infrastructure services: %w", err)
	}

	// 4. Initialize domain services (depend on repositories and infrastructure)
	if err := f.createDomainServices(components); err != nil {
		return nil, fmt.Errorf("failed to create domain services: %w", err)
	}

	// 5. Wire peer management service to node service (after both are created)
	f.wirePeerManagementService(components)

	// 6. Initialize application services (depend on domain services)
	if err := f.createApplicationServices(components); err != nil {
		return nil, fmt.Errorf("failed to create application services: %w", err)
	}

	f.logger.Info("all service components created successfully")
	return components, nil
}

// createDatabaseStore initializes the database store
func (f *ServiceFactory) createDatabaseStore(components *ServiceComponents) error {
	f.logger.Debug("initializing database store")

	store, err := db.NewStore(&db.Config{
		Path:            f.config.DB.Path,
		MaxOpenConns:    f.config.DB.MaxOpenConns,
		MaxIdleConns:    f.config.DB.MaxIdleConns,
		ConnMaxLifetime: int(time.Duration(f.config.DB.ConnMaxLifetime) * time.Second),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize database store: %w", err)
	}

	components.Store = store
	f.logger.Debug("database store initialized successfully")
	return nil
}

// createRepositories initializes all repository implementations
func (f *ServiceFactory) createRepositories(components *ServiceComponents) error {
	f.logger.Debug("initializing repositories")

	// Node repository
	components.NodeRepository = store.NewNodeRepository(components.Store)

	// Peer repository
	components.PeerRepository = store.NewPeerRepository(components.Store)

	// Subnet repository
	components.SubnetRepository = store.NewSubnetRepository(components.Store)

	f.logger.Debug("repositories initialized successfully")
	return nil
}

// createInfrastructureServices initializes infrastructure layer services
func (f *ServiceFactory) createInfrastructureServices(components *ServiceComponents) error {
	f.logger.Debug("initializing infrastructure services")

	// Initialize cloud provisioner
	if err := f.createCloudProvisioner(components); err != nil {
		return fmt.Errorf("failed to create cloud provisioner: %w", err)
	}

	// Initialize node interactor
	if err := f.createNodeInteractor(components); err != nil {
		return fmt.Errorf("failed to create node interactor: %w", err)
	}

	f.logger.Debug("infrastructure services initialized successfully")
	return nil
}

// createCloudProvisioner initializes the cloud provisioner
func (f *ServiceFactory) createCloudProvisioner(components *ServiceComponents) error {
	f.logger.Debug("initializing cloud provisioner")

	// Create Hetzner provisioner
	hetznerConfig := &provisioner.HetznerConfig{
		ServerType:   f.getDefaultServerType(),
		Image:        f.config.Hetzner.Image,
		Location:     f.getDefaultLocation(),
		SSHPublicKey: f.config.Hetzner.SSHKey,
	}

	provisioner, err := provisioner.NewHetznerProvisioner(f.config.Hetzner.APIToken, hetznerConfig, f.logger)
	if err != nil {
		return fmt.Errorf("failed to create Hetzner provisioner: %w", err)
	}

	components.CloudProvisioner = provisioner
	f.logger.Debug("cloud provisioner initialized successfully")
	return nil
}

// createNodeInteractor initializes the node interactor
func (f *ServiceFactory) createNodeInteractor(components *ServiceComponents) error {
	f.logger.Debug("initializing node interactor")

	// Read SSH private key
	privateKeyBytes, err := os.ReadFile(f.config.Hetzner.SSHPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH private key from %s: %w", f.config.Hetzner.SSHPrivateKeyPath, err)
	}

	// Create node interactor configuration
	nodeInteractorConfig := f.createNodeInteractorConfig()

	// Create SSH node interactor
	nodeInteractor, err := nodeinteractor.NewSSHNodeInteractor(
		string(privateKeyBytes),
		nodeInteractorConfig,
		f.logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create SSH node interactor: %w", err)
	}

	components.NodeInteractor = nodeInteractor
	f.logger.Debug("node interactor initialized successfully")
	return nil
}

// createDomainServices initializes domain layer services
func (f *ServiceFactory) createDomainServices(components *ServiceComponents) error {
	f.logger.Debug("initializing domain services")

	// Initialize IP service
	if err := f.createIPService(components); err != nil {
		return fmt.Errorf("failed to create IP service: %w", err)
	}

	// Initialize peer service
	if err := f.createPeerService(components); err != nil {
		return fmt.Errorf("failed to create peer service: %w", err)
	}

	// Initialize node service
	if err := f.createNodeService(components); err != nil {
		return fmt.Errorf("failed to create node service: %w", err)
	}

	// Initialize peer management service
	if err := f.createPeerManagementService(components); err != nil {
		return fmt.Errorf("failed to create peer management service: %w", err)
	}

	// Initialize provisioning service
	if err := f.createProvisioningService(components); err != nil {
		return fmt.Errorf("failed to create provisioning service: %w", err)
	}

	f.logger.Debug("domain services initialized successfully")
	return nil
}

// createIPService initializes the IP domain service
func (f *ServiceFactory) createIPService(components *ServiceComponents) error {
	f.logger.Debug("initializing IP service")

	ipConfig := ip.DefaultNetworkConfig()
	ipService, err := ip.NewService(components.SubnetRepository, ipConfig, f.logger)
	if err != nil {
		return fmt.Errorf("failed to create IP service: %w", err)
	}

	components.IPService = ipService
	f.logger.Debug("IP service initialized successfully")
	return nil
}

// createPeerService initializes the peer domain service
func (f *ServiceFactory) createPeerService(components *ServiceComponents) error {
	f.logger.Debug("initializing peer service")

	peerService := peer.NewService(components.PeerRepository)
	components.PeerService = peerService

	f.logger.Debug("peer service initialized successfully")
	return nil
}

// createNodeService initializes the node domain service
func (f *ServiceFactory) createNodeService(components *ServiceComponents) error {
	f.logger.Debug("initializing node service")

	nodeServiceConfig := f.createNodeServiceConfig()

	nodeService := node.NewService(
		components.NodeRepository,
		components.CloudProvisioner,
		components.NodeInteractor,
		f.logger,
		nodeServiceConfig,
	)

	components.NodeService = nodeService
	f.logger.Debug("node service initialized successfully")
	return nil
}

// wirePeerManagementService wires the peer management service to the node service after both are created
func (f *ServiceFactory) wirePeerManagementService(components *ServiceComponents) {
	if components.NodeService != nil && components.PeerManagementService != nil {
		// Cast to concrete type to access SetPeerManagementService method
		if nodeService, ok := components.NodeService.(*node.Service); ok {
			nodeService.SetPeerManagementService(components.PeerManagementService)
			f.logger.Debug("peer management service wired to node service")
		}
	}
}

// createApplicationServices initializes application layer services
func (f *ServiceFactory) createApplicationServices(components *ServiceComponents) error {
	f.logger.Debug("initializing application services")

	// Initialize node provisioning service
	nodeProvisioningService := application.NewNodeProvisioningService(
		components.ProvisioningService,
		components.NodeService,
		f.logger,
	)

	// Initialize VPN orchestrator service (uses smaller focused services)
	vpnService := application.NewVPNOrchestratorService(
		components.NodeService,
		components.PeerService,
		components.IPService,
		nodeProvisioningService,
		f.logger,
	)
	components.VPNService = vpnService

	// Initialize admin service
	adminService := application.NewAdminService(
		components.NodeService,
		components.PeerService,
		components.IPService,
		vpnService,
		f.logger,
	)
	components.AdminService = adminService

	f.logger.Debug("application services initialized successfully")
	return nil
}

// createPeerManagementService initializes the peer management service
func (f *ServiceFactory) createPeerManagementService(components *ServiceComponents) error {
	f.logger.Debug("initializing peer management service")

	peerManagementConfig := f.createPeerManagementConfig()

	peerManagementService := node.NewPeerManagementService(
		components.NodeService,
		components.NodeRepository,
		components.NodeInteractor,
		f.logger,
		peerManagementConfig,
	)

	components.PeerManagementService = peerManagementService
	f.logger.Debug("peer management service initialized successfully")
	return nil
}

// createPeerManagementConfig creates peer management configuration
func (f *ServiceFactory) createPeerManagementConfig() node.PeerManagementConfig {
	return node.PeerManagementConfig{
		MaxPeersPerNode:       50,
		ValidateBeforeAdd:     true,
		ValidateAfterAdd:      true,
		ValidateAfterRemove:   true,
		SyncConfigAfterChange: true,
		AllowDuplicateIPs:     false,
		RequirePresharedKey:   false,
	}
}

// Helper methods for configuration

// createNodeServiceConfig creates node service configuration from config
func (f *ServiceFactory) createNodeServiceConfig() node.ServiceConfig {
	// Use config values if available, otherwise use defaults
	config := node.ServiceConfig{
		MaxPeersPerNode:      50,
		CapacityThreshold:    80.0,
		HealthCheckTimeout:   30 * time.Second,
		ProvisioningTimeout:  10 * time.Minute,
		DestructionTimeout:   5 * time.Minute,
		OptimalNodeSelection: "least_loaded",
	}

	// Override with config values if provided
	if f.config.NodeService.MaxPeersPerNode > 0 {
		config.MaxPeersPerNode = f.config.NodeService.MaxPeersPerNode
	}
	if f.config.NodeService.CapacityThreshold > 0 {
		config.CapacityThreshold = f.config.NodeService.CapacityThreshold
	}
	if f.config.NodeService.HealthCheckTimeout > 0 {
		config.HealthCheckTimeout = f.config.NodeService.HealthCheckTimeout
	}
	if f.config.NodeService.ProvisioningTimeout > 0 {
		config.ProvisioningTimeout = f.config.NodeService.ProvisioningTimeout
	}
	if f.config.NodeService.DestructionTimeout > 0 {
		config.DestructionTimeout = f.config.NodeService.DestructionTimeout
	}
	if f.config.NodeService.OptimalNodeSelection != "" {
		config.OptimalNodeSelection = f.config.NodeService.OptimalNodeSelection
	}

	return config
}

// createNodeInteractorConfig creates node interactor configuration from config
func (f *ServiceFactory) createNodeInteractorConfig() nodeinteractor.NodeInteractorConfig {
	// Use config values if available, otherwise use defaults
	config := nodeinteractor.NodeInteractorConfig{
		SSHTimeout:          30 * time.Second,
		CommandTimeout:      60 * time.Second,
		WireGuardConfigPath: "/etc/wireguard/wg0.conf",
		WireGuardInterface:  "wg0",
		RetryAttempts:       3,
		RetryBackoff:        2 * time.Second,
		HealthCheckCommands: f.getDefaultHealthCheckCommands(),
		SystemMetricsPath:   f.getDefaultSystemMetricsPaths(),
		CircuitBreaker: nodeinteractor.CircuitBreakerConfig{
			FailureThreshold: 5,
			ResetTimeout:     60 * time.Second,
			MaxAttempts:      3,
		},
	}

	// Override with config values if provided
	if f.config.NodeInteractor.SSHTimeout > 0 {
		config.SSHTimeout = f.config.NodeInteractor.SSHTimeout
	}
	if f.config.NodeInteractor.CommandTimeout > 0 {
		config.CommandTimeout = f.config.NodeInteractor.CommandTimeout
	}
	if f.config.NodeInteractor.WireGuardConfigPath != "" {
		config.WireGuardConfigPath = f.config.NodeInteractor.WireGuardConfigPath
	}
	if f.config.NodeInteractor.WireGuardInterface != "" {
		config.WireGuardInterface = f.config.NodeInteractor.WireGuardInterface
	}
	if f.config.NodeInteractor.RetryAttempts > 0 {
		config.RetryAttempts = f.config.NodeInteractor.RetryAttempts
	}
	if f.config.NodeInteractor.RetryBackoff > 0 {
		config.RetryBackoff = f.config.NodeInteractor.RetryBackoff
	}
	if len(f.config.NodeInteractor.HealthCheckCommands) > 0 {
		config.HealthCheckCommands = f.convertHealthCheckCommands(f.config.NodeInteractor.HealthCheckCommands)
	}

	// Override circuit breaker config if provided
	if f.config.CircuitBreaker.FailureThreshold > 0 {
		config.CircuitBreaker.FailureThreshold = f.config.CircuitBreaker.FailureThreshold
	}
	if f.config.CircuitBreaker.ResetTimeout > 0 {
		config.CircuitBreaker.ResetTimeout = f.config.CircuitBreaker.ResetTimeout
	}
	if f.config.CircuitBreaker.MaxAttempts > 0 {
		config.CircuitBreaker.MaxAttempts = f.config.CircuitBreaker.MaxAttempts
	}

	return config
}

// getDefaultServerType returns the first configured server type or a default
func (f *ServiceFactory) getDefaultServerType() string {
	if len(f.config.Hetzner.ServerTypes) > 0 {
		return f.config.Hetzner.ServerTypes[0]
	}
	return "cx11"
}

// getDefaultLocation returns the first configured location or a default
func (f *ServiceFactory) getDefaultLocation() string {
	if len(f.config.Hetzner.Locations) > 0 {
		return f.config.Hetzner.Locations[0]
	}
	return "nbg1"
}

// getDefaultHealthCheckCommands returns default health check commands
func (f *ServiceFactory) getDefaultHealthCheckCommands() []nodeinteractor.HealthCommand {
	return []nodeinteractor.HealthCommand{
		{
			Name:         "system_load",
			Command:      "cat /proc/loadavg",
			Timeout:      5 * time.Second,
			Critical:     false,
			ExpectedExit: 0,
		},
		{
			Name:         "memory_info",
			Command:      "cat /proc/meminfo",
			Timeout:      5 * time.Second,
			Critical:     false,
			ExpectedExit: 0,
		},
		{
			Name:         "disk_usage",
			Command:      "df -h /",
			Timeout:      10 * time.Second,
			Critical:     false,
			ExpectedExit: 0,
		},
		{
			Name:         "wireguard_status",
			Command:      "systemctl is-active wg-quick@wg0",
			Timeout:      5 * time.Second,
			Critical:     true,
			ExpectedExit: 0,
		},
	}
}

// getDefaultSystemMetricsPaths returns default system metrics paths
func (f *ServiceFactory) getDefaultSystemMetricsPaths() nodeinteractor.SystemMetricsPaths {
	return nodeinteractor.SystemMetricsPaths{
		LoadAvg:   "/proc/loadavg",
		MemInfo:   "/proc/meminfo",
		DiskUsage: "df -h /",
		Uptime:    "/proc/uptime",
		Hostname:  "/etc/hostname",
		OSRelease: "/etc/os-release",
	}
}

// convertHealthCheckCommands converts config health check commands to nodeinteractor format
func (f *ServiceFactory) convertHealthCheckCommands(configCommands []config.HealthCommand) []nodeinteractor.HealthCommand {
	commands := make([]nodeinteractor.HealthCommand, len(configCommands))
	for i, cmd := range configCommands {
		commands[i] = nodeinteractor.HealthCommand{
			Name:         cmd.Name,
			Command:      cmd.Command,
			Timeout:      cmd.Timeout,
			Critical:     cmd.Critical,
			ExpectedExit: cmd.ExpectedExit,
		}
	}
	return commands
}

// createProvisioningService initializes the provisioning service
func (f *ServiceFactory) createProvisioningService(components *ServiceComponents) error {
	f.logger.Debug("initializing provisioning service")

	provisioningConfig := f.createProvisioningServiceConfig()

	provisioningService := node.NewProvisioningService(
		components.NodeService,
		components.NodeRepository,
		components.CloudProvisioner,
		components.NodeInteractor,
		components.IPService,
		f.logger,
		provisioningConfig,
	)

	components.ProvisioningService = provisioningService
	f.logger.Debug("provisioning service initialized successfully")
	return nil
}

// createProvisioningServiceConfig creates provisioning service configuration
func (f *ServiceFactory) createProvisioningServiceConfig() node.ProvisioningServiceConfig {
	return node.ProvisioningServiceConfig{
		ProvisioningTimeout:    10 * time.Minute,
		ReadinessTimeout:       5 * time.Minute,
		ReadinessCheckInterval: 30 * time.Second,
		MaxRetryAttempts:       3,
		RetryBackoff:           2 * time.Second,
		CleanupOnFailure:       true,
		DefaultRegion:          f.getDefaultLocation(),
		DefaultInstanceType:    f.getDefaultServerType(),
		DefaultImageID:         f.config.Hetzner.Image,
		DefaultSSHKey:          f.config.Hetzner.SSHKey,
		DefaultTags: map[string]string{
			"service": "vpn-rotator",
			"type":    "vpn-node",
		},
	}
}
