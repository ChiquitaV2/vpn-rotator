package rotator

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/application"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	sharedevents "github.com/chiquitav2/vpn-rotator/internal/shared/events"
)

// ServiceFactory creates and wires all service components with proper dependency injection.
type ServiceFactory struct {
	config           *config.Config
	internalDefaults *config.InternalDefaults
	logger           *slog.Logger
}

// NewServiceFactory creates a new service factory.
func NewServiceFactory(cfg *config.Config, logger *slog.Logger) *ServiceFactory {
	return &ServiceFactory{
		config:           cfg,
		internalDefaults: config.NewInternalDefaults(),
		logger:           logger,
	}
}

// ServiceComponents holds all the service components.
type ServiceComponents struct {
	// Infrastructure layer
	Store            db.Store
	CloudProvisioner provisioner.CloudProvisioner
	NodeInteractor   nodeinteractor.NodeInteractor

	// Event system
	EventBus         sharedevents.EventBus
	EventPublisher   *events.EventPublisher
	ProgressReporter events.ProgressReporter

	// Domain services
	NodeService         node.NodeService
	PeerService         peer.Service
	IPService           ip.Service
	ProvisioningService *node.ProvisioningService

	// Application services
	VPNService               application.VPNService
	AdminService             application.AdminService
	ProvisioningOrchestrator *application.ProvisioningOrchestrator

	// Repositories
	NodeRepository   node.NodeRepository
	PeerRepository   peer.Repository
	SubnetRepository ip.Repository
}

// CreateComponents creates all service components with proper dependency injection.
func (f *ServiceFactory) CreateComponents() (*ServiceComponents, error) {
	f.logger.Info("creating service components")

	components := &ServiceComponents{}

	// Initialization order is critical due to dependencies.
	// 1. Foundational layers (DB, repositories)
	// 2. Infrastructure services (provisioner, interactor)
	// 3. Event system
	// 4. Domain services (depend on previous layers)
	// 5. Application services (depend on all previous layers)

	steps := []func(*ServiceComponents) error{
		f.createDatabaseStore,
		f.createRepositories,
		f.createInfrastructureServices,
		f.createEventSystem,
		f.createDomainServices,
		f.createApplicationServices,
	}

	for _, step := range steps {
		if err := step(components); err != nil {
			return nil, err
		}
	}

	f.logger.Info("all service components created successfully")
	return components, nil
}

func (f *ServiceFactory) createDatabaseStore(c *ServiceComponents) error {
	f.logger.Debug("initializing database store")

	// Get database defaults from internal configuration
	dbDefaults := f.internalDefaults.DatabaseDefaults()

	store, err := db.NewStore(&db.Config{
		Path:            f.config.Database.Path,
		MaxOpenConns:    dbDefaults.MaxOpenConns,
		MaxIdleConns:    dbDefaults.MaxIdleConns,
		ConnMaxLifetime: int(dbDefaults.ConnMaxLifetime.Seconds()),
	})
	if err != nil {
		return fmt.Errorf("failed to initialize database store: %w", err)
	}
	c.Store = store
	return nil
}

func (f *ServiceFactory) createRepositories(c *ServiceComponents) error {
	f.logger.Debug("initializing repositories")
	c.NodeRepository = store.NewNodeRepository(c.Store)
	c.PeerRepository = store.NewPeerRepository(c.Store)
	c.SubnetRepository = store.NewSubnetRepository(c.Store)
	return nil
}

func (f *ServiceFactory) createInfrastructureServices(c *ServiceComponents) error {
	f.logger.Debug("initializing infrastructure services")
	if err := f.createCloudProvisioner(c); err != nil {
		return err
	}
	return f.createNodeInteractor(c)
}

func (f *ServiceFactory) createCloudProvisioner(c *ServiceComponents) error {
	f.logger.Debug("initializing cloud provisioner")
	hetzerConfig := &provisioner.HetznerConfig{
		ServerType:   f.getDefaultLocation(),
		Image:        f.config.Hetzner.Image,
		Location:     f.getDefaultLocation(),
		SSHPublicKey: f.config.Hetzner.SSHKey,
	}
	hetzerProvisioner, err := provisioner.NewHetznerProvisioner(f.config.Hetzner.APIToken, hetzerConfig, f.logger)
	if err != nil {
		return fmt.Errorf("failed to create Hetzner provisioner: %w", err)
	}
	c.CloudProvisioner = hetzerProvisioner
	return nil
}

func (f *ServiceFactory) createNodeInteractor(c *ServiceComponents) error {
	f.logger.Debug("initializing node interactor")
	privateKey, err := os.ReadFile(f.config.Hetzner.SSHPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH private key: %w", err)
	}

	interactorConfig := f.createNodeInteractorConfig()
	interactor, err := nodeinteractor.NewSSHNodeInteractor(string(privateKey), interactorConfig, f.logger)
	if err != nil {
		return fmt.Errorf("failed to create SSH node interactor: %w", err)
	}
	c.NodeInteractor = interactor
	return nil
}

func (f *ServiceFactory) createEventSystem(c *ServiceComponents) error {
	f.logger.Debug("initializing event system")
	eventBusConfig := f.createEventBusConfig()
	eventBus := sharedevents.NewGookitEventBus(eventBusConfig, f.logger)
	eventPublisher := events.NewEventPublisher(eventBus)

	c.EventBus = eventBus
	c.EventPublisher = eventPublisher
	c.ProgressReporter = events.NewEventBasedProgressReporter(eventPublisher.Provisioning)
	return nil
}

func (f *ServiceFactory) createDomainServices(c *ServiceComponents) error {
	f.logger.Debug("initializing domain services")
	ipConfig := ip.DefaultNetworkConfig()
	ipService, err := ip.NewService(c.SubnetRepository, ipConfig, f.logger)
	if err != nil {
		return fmt.Errorf("failed to create IP service: %w", err)
	}
	c.IPService = ipService
	c.PeerService = peer.NewService(c.PeerRepository)

	nodeSvcConfig := f.createNodeServiceConfig()
	c.NodeService = node.NewService(c.NodeRepository, c.CloudProvisioner, c.NodeInteractor, c.NodeInteractor, f.logger, nodeSvcConfig)

	return f.createProvisioningService(c)
}

func (f *ServiceFactory) createProvisioningService(c *ServiceComponents) error {
	f.logger.Debug("initializing provisioning service")
	provConfig := f.createProvisioningServiceConfig()
	provService := node.NewProvisioningService(
		c.NodeService,
		c.NodeRepository,
		c.CloudProvisioner,
		c.NodeInteractor,
		c.IPService,
		f.logger,
		provConfig,
	)
	provService.SetProgressReporter(c.ProgressReporter)
	c.ProvisioningService = provService
	return nil
}

func (f *ServiceFactory) createApplicationServices(c *ServiceComponents) error {
	f.logger.Debug("initializing application services")
	orchestratorConfig := f.createOrchestratorConfig()
	provOrchestrator := application.NewProvisioningOrchestratorWithConfig(
		c.ProvisioningService,
		c.NodeService,
		c.EventPublisher.Provisioning,
		orchestratorConfig,
		f.logger,
	)
	c.ProvisioningOrchestrator = provOrchestrator

	vpnService := application.NewVPNOrchestratorService(
		c.NodeService,
		c.PeerService,
		c.IPService,
		c.NodeInteractor,
		c.ProvisioningOrchestrator,
		f.logger,
	)
	c.VPNService = vpnService

	adminService := application.NewAdminService(
		c.NodeService,
		c.PeerService,
		c.IPService,
		vpnService,
		c.ProvisioningOrchestrator,
		f.logger,
	)
	c.AdminService = adminService
	return nil
}

// --- Configuration Helpers ---

func (f *ServiceFactory) createNodeServiceConfig() node.ServiceConfig {
	// Use peer settings from user config combined with internal defaults
	nodeDefaults := f.internalDefaults.NodeServiceDefaults()
	return node.ServiceConfig{
		MaxPeersPerNode:      f.config.Peers.MaxPerNode,
		CapacityThreshold:    f.config.Peers.CapacityThreshold,
		HealthCheckTimeout:   nodeDefaults.HealthCheckTimeout,
		ProvisioningTimeout:  nodeDefaults.ProvisioningTimeout,
		DestructionTimeout:   nodeDefaults.DestructionTimeout,
		OptimalNodeSelection: nodeDefaults.OptimalNodeSelection,
	}
}

func (f *ServiceFactory) createNodeInteractorConfig() nodeinteractor.NodeInteractorConfig {
	// Use internal defaults for node interactor configuration
	nodeDefaults := f.internalDefaults.NodeInteractorDefaults()
	breakerDefaults := f.internalDefaults.CircuitBreakerDefaults()

	return nodeinteractor.NodeInteractorConfig{
		SSHTimeout:          nodeDefaults.SSHTimeout,
		CommandTimeout:      nodeDefaults.CommandTimeout,
		WireGuardConfigPath: nodeDefaults.WireGuardConfigPath,
		WireGuardInterface:  nodeDefaults.WireGuardInterface,
		RetryAttempts:       nodeDefaults.RetryAttempts,
		RetryBackoff:        nodeDefaults.RetryBackoff,
		HealthCheckCommands: nodeinteractor.DefaultConfig().HealthCheckCommands,
		CircuitBreaker: nodeinteractor.CircuitBreakerConfig{
			FailureThreshold: breakerDefaults.FailureThreshold,
			ResetTimeout:     breakerDefaults.ResetTimeout,
			MaxAttempts:      breakerDefaults.MaxAttempts,
		},
	}
}

func (f *ServiceFactory) createProvisioningServiceConfig() node.ProvisioningServiceConfig {
	// Use centralized internal defaults for provisioning configuration
	eventDefaults := f.internalDefaults.EventSystemDefaults()
	nodeDefaults := f.internalDefaults.NodeInteractorDefaults()
	provDefaults := f.internalDefaults.ProvisioningDefaults()

	return node.ProvisioningServiceConfig{
		ProvisioningTimeout:    eventDefaults.WorkerTimeout,
		ReadinessTimeout:       provDefaults.ReadinessTimeout,
		ReadinessCheckInterval: provDefaults.ReadinessCheckInterval,
		MaxRetryAttempts:       nodeDefaults.RetryAttempts,
		RetryBackoff:           nodeDefaults.RetryBackoff,
		CleanupOnFailure:       provDefaults.CleanupOnFailure,
		DefaultRegion:          f.getDefaultLocation(),
		DefaultInstanceType:    f.getDefaultServerType(),
		DefaultImageID:         f.config.Hetzner.Image,
		DefaultSSHKey:          f.config.Hetzner.SSHKey,
		DefaultTags:            provDefaults.DefaultTags,
	}
}

func (f *ServiceFactory) createEventBusConfig() sharedevents.EventBusConfig {
	eventDefaults := f.internalDefaults.EventSystemDefaults()
	return sharedevents.EventBusConfig{
		Mode:       "simple", // Hardcoded internal default
		Timeout:    eventDefaults.WorkerTimeout,
		MaxRetries: 3, // Hardcoded internal default
	}
}

func (f *ServiceFactory) createOrchestratorConfig() application.OrchestratorConfig {
	eventDefaults := f.internalDefaults.EventSystemDefaults()
	return application.OrchestratorConfig{
		WorkerTimeout:          eventDefaults.WorkerTimeout,
		ETAHistoryRetention:    eventDefaults.ETAHistoryRetention,
		DefaultProvisioningETA: eventDefaults.DefaultProvisioningETA,
		MaxConcurrentJobs:      eventDefaults.MaxConcurrentJobs,
	}
}

func (f *ServiceFactory) getDefaultServerType() string {
	if len(f.config.Hetzner.ServerTypes) > 0 {
		return f.config.Hetzner.ServerTypes[0]
	}
	return "cx11" // Default fallback
}

func (f *ServiceFactory) getDefaultLocation() string {
	if len(f.config.Hetzner.Locations) > 0 {
		return f.config.Hetzner.Locations[0]
	}
	return "nbg1" // Default fallback
}

// Health check commands are now handled by internal defaults in nodeinteractor.DefaultConfig()
// This function is no longer needed as health checks are not user-configurable
