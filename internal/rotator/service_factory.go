package rotator

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/chiquitav2/vpn-rotator/internal/rotator/config"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/events"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/protocols"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/provisioner"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/remote"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/store/db"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/ip"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/node"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/orchestrator"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/peer"
	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	"github.com/chiquitav2/vpn-rotator/pkg/errors"
	sharedevents "github.com/chiquitav2/vpn-rotator/pkg/events"
	"github.com/chiquitav2/vpn-rotator/pkg/logger"
)

// ServiceFactory creates and wires all service components with proper dependency injection.
type ServiceFactory struct {
	config           *config.Config
	internalDefaults *config.InternalDefaults
	logger           *logger.Logger
}

// NewServiceFactory creates a new service factory.
func NewServiceFactory(cfg *config.Config, logger *logger.Logger) *ServiceFactory {
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
	CloudProvisioner services.CloudProvisioner
	NodeInteractor   remote.NodeInteractor

	// Event system
	EventBus         sharedevents.EventBus
	EventPublisher   *events.EventPublisher
	ProgressReporter events.ProgressReporter

	// Domain services
	NodeService           node.NodeService
	PeerService           peer.Service
	IPService             ip.Service
	PeerConnectionTracker *peer.PeerConnectionStateTracker

	// Application services
	PeerConnectionSvr      *services.PeerConnectionService
	AdminOrchestrator      orchestrator.AdminOrchestrator
	ProvisioningService    *services.ProvisioningService
	NodeRotatorService     *services.NodeRotationService
	ResourceCleanupService *services.ResourceCleanupService

	// Orchestrators
	NodeOrchestrator orchestrator.NodeOrchestrator
	SchedulerAdapter *orchestrator.SchedulerVPNService

	// Repositories
	NodeRepository   node.NodeRepository
	PeerRepository   peer.Repository
	SubnetRepository ip.Repository
}

// CreateComponents creates all service components with proper dependency injection.
func (f *ServiceFactory) CreateComponents() (*ServiceComponents, error) {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_components")

	components := &ServiceComponents{}

	// Initialization order is critical due to dependencies.
	// 1. Foundational layers (DB, repositories)
	// 2. Infrastructure services (provisioner, interactor)
	// 3. Event system
	// 4. domain services (depend on previous layers)
	// 5. Application services (depend on all previous layers)

	stepConfigs := []struct {
		name string
		fn   func(*ServiceComponents) error
	}{
		{"database_store", f.createDatabaseStore},
		{"repositories", f.createRepositories},
		{"infrastructure_services", f.createInfrastructureServices},
		{"event_system", f.createEventSystem},
		{"domain_services", f.createDomainServices},
		{"application_services", f.createApplicationServices},
	}

	for _, stepConfig := range stepConfigs {
		op.With(slog.String("step", stepConfig.name))
		op.Progress("creating component", slog.String("step", stepConfig.name))

		if err := stepConfig.fn(components); err != nil {
			factoryErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
				fmt.Sprintf("failed to create %s", stepConfig.name), false)
			f.logger.ErrorCtx(ctx, "component creation failed", factoryErr,
				slog.String("step", stepConfig.name))
			op.Fail(factoryErr, "component creation failed")
			return nil, factoryErr
		}
	}

	op.Complete("all service components created successfully")
	return components, nil
}

func (f *ServiceFactory) createDatabaseStore(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_database_store")

	// Get database defaults from internal configuration
	dbDefaults := f.internalDefaults.DatabaseDefaults()
	dbPath := f.config.Database.Path

	f.logger.DebugContext(ctx, "initializing database store",
		"path", dbPath,
		"max_open_conns", dbDefaults.MaxOpenConns,
		"max_idle_conns", dbDefaults.MaxIdleConns,
		"conn_max_lifetime", dbDefaults.ConnMaxLifetime)

	store, err := db.NewStore(&db.Config{
		Path:            dbPath,
		MaxOpenConns:    dbDefaults.MaxOpenConns,
		MaxIdleConns:    dbDefaults.MaxIdleConns,
		ConnMaxLifetime: int(dbDefaults.ConnMaxLifetime.Seconds()),
	})
	if err != nil {
		dbError := errors.WrapWithDomain(err, errors.DomainDatabase, errors.ErrCodeInternal,
			"failed to initialize database store", false)
		f.logger.ErrorCtx(ctx, "database store creation failed", dbError, "path", dbPath)
		op.Fail(dbError, "database store creation failed")
		return dbError
	}

	c.Store = store
	op.Complete("database store initialized successfully", "path", dbPath)
	return nil
}

func (f *ServiceFactory) createRepositories(c *ServiceComponents) error {
	ctx := context.Background()

	repoOperation := f.logger.StartOp(ctx, "create_repositories", "service_factory")

	f.logger.DebugContext(ctx, "initializing repositories")

	c.NodeRepository = store.NewNodeRepository(c.Store, f.logger)
	c.PeerRepository = store.NewPeerRepository(c.Store, f.logger)
	c.SubnetRepository = store.NewSubnetRepository(c.Store, f.logger)

	repoOperation.Complete("repositories created successfully")
	f.logger.DebugContext(ctx, "repositories initialized successfully")
	return nil
}

func (f *ServiceFactory) createInfrastructureServices(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_infrastructure_services")

	f.logger.DebugContext(ctx, "initializing infrastructure services")

	if err := f.createCloudProvisioner(c); err != nil {
		infraErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
			"failed to create cloud provisioner", false)
		f.logger.ErrorCtx(ctx, "cloud provisioner creation failed", infraErr)
		op.Fail(infraErr, "cloud provisioner creation failed")
		return infraErr
	}

	if err := f.createNodeInteractor(c); err != nil {
		infraErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
			"failed to create node interactor", false)
		f.logger.ErrorCtx(ctx, "node interactor creation failed", infraErr)
		op.Fail(infraErr, "node interactor creation failed")
		return infraErr
	}

	op.Complete("infrastructure services initialized successfully")
	return nil
}

func (f *ServiceFactory) createCloudProvisioner(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_cloud_provisioner")

	defaultLocation := f.getDefaultLocation()

	f.logger.DebugContext(ctx, "initializing cloud provisioner",
		"server_type", defaultLocation,
		"image", f.config.Hetzner.Image,
		"location", defaultLocation)

	hetzerConfig := &provisioner.HetznerConfig{
		ServerType:   defaultLocation,
		Image:        f.config.Hetzner.Image,
		Location:     defaultLocation,
		SSHPublicKey: f.config.Hetzner.SSHKey,
	}

	hetzerProvisioner, err := provisioner.NewHetznerProvisioner(f.config.Hetzner.APIToken, hetzerConfig, f.logger)
	if err != nil {
		provisionerErr := errors.WrapWithDomain(err, errors.DomainProvisioning, errors.ErrCodeProvisionFailed,
			"failed to create Hetzner provisioner", false)
		f.logger.ErrorCtx(ctx, "Hetzner provisioner creation failed", provisionerErr,
			slog.String("location", defaultLocation), slog.String("image", f.config.Hetzner.Image))
		op.Fail(provisionerErr, "Hetzner provisioner creation failed")
		return provisionerErr
	}

	c.CloudProvisioner = hetzerProvisioner
	op.Complete("cloud provisioner initialized successfully", "location", defaultLocation)
	return nil
}

func (f *ServiceFactory) createNodeInteractor(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_node_interactor")

	sshKeyPath := f.config.Hetzner.SSHPrivateKeyPath
	f.logger.DebugContext(ctx, "initializing node interactor", "ssh_key_path", sshKeyPath)

	privateKey, err := os.ReadFile(sshKeyPath)
	if err != nil {
		interactorErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
			"failed to read SSH private key", false)
		f.logger.ErrorCtx(ctx, "SSH private key read failed", interactorErr, "path", sshKeyPath)
		op.Fail(interactorErr, "SSH private key read failed")
		return interactorErr
	}

	interactorConfig := f.createNodeInteractorConfig()
	interactor, err := remote.NewSSHNodeInteractor(string(privateKey), interactorConfig, f.logger)
	if err != nil {
		interactorErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
			"failed to create SSH node interactor", false)
		f.logger.ErrorCtx(ctx, "SSH node interactor creation failed", interactorErr)
		op.Fail(interactorErr, "SSH node interactor creation failed")
		return interactorErr
	}

	c.NodeInteractor = interactor
	op.Complete("node interactor initialized successfully")
	return nil
}

func (f *ServiceFactory) createEventSystem(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_event_system")

	f.logger.DebugContext(ctx, "initializing event system")

	eventBusConfig := f.createEventBusConfig()
	eventBus := sharedevents.NewGookitEventBus(eventBusConfig, f.logger)
	eventPublisher := events.NewEventPublisher(eventBus, f.logger)

	c.EventBus = eventBus
	c.EventPublisher = eventPublisher
	c.ProgressReporter = events.NewEventBasedProgressReporter(eventPublisher.Provisioning, f.logger)

	op.Complete("event system initialized successfully")
	return nil
}

func (f *ServiceFactory) createDomainServices(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_domain_services")

	f.logger.DebugContext(ctx, "initializing domain services")

	// Create IP service
	ipConfig := ip.DefaultNetworkConfig()
	ipService, err := ip.NewService(c.SubnetRepository, ipConfig, f.logger)
	if err != nil {
		ipErr := errors.WrapWithDomain(err, errors.DomainSystem, errors.ErrCodeInternal,
			"failed to create IP service", false)
		f.logger.ErrorCtx(ctx, "IP service creation failed", ipErr)
		op.Fail(ipErr, "IP service creation failed")
		return ipErr
	}
	c.IPService = ipService
	f.logger.DebugContext(ctx, "IP service created successfully")

	// Create peer service
	c.PeerService = peer.NewService(c.PeerRepository, f.logger)
	f.logger.DebugContext(ctx, "peer service created successfully")

	// Create peer connection state tracker
	c.PeerConnectionTracker = peer.NewPeerConnectionStateTracker(c.EventPublisher.Peer, f.logger)
	f.logger.DebugContext(ctx, "peer connection state tracker created successfully")

	// Create node service
	nodeSvcConfig := f.createNodeServiceConfig()
	c.NodeService = node.NewService(c.NodeRepository, c.CloudProvisioner, c.NodeInteractor, c.NodeInteractor, f.logger, nodeSvcConfig)
	f.logger.DebugContext(ctx, "node service created successfully")

	op.Complete("domain services initialized successfully")
	return nil
}

func (f *ServiceFactory) createApplicationServices(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_application_services")

	f.logger.DebugContext(ctx, "initializing application services")

	// Create provisioning service
	if err := f.createProvisioningService(c); err != nil {
		provErr := errors.WrapWithDomain(err, errors.DomainProvisioning, errors.ErrCodeInternal,
			"failed to create provisioning service", false)
		f.logger.ErrorCtx(ctx, "provisioning service creation failed", provErr)
		op.Fail(provErr, "provisioning service creation failed")
		return provErr
	}

	// Protocol manager adapter (WireGuard)
	protoMgr := protocols.NewWireGuardProtocolManager(c.NodeInteractor)

	// Create peer connection service
	c.PeerConnectionSvr = services.NewPeerConnectionService(
		c.NodeService,
		c.PeerService,
		c.IPService,
		protoMgr,
		c.EventPublisher,
		c.PeerConnectionTracker,
		f.logger,
	)
	f.logger.DebugContext(ctx, "peer connection service created successfully")

	// Start async connection worker
	go c.PeerConnectionSvr.StartConnectionWorker(ctx)
	f.logger.DebugContext(ctx, "peer connection worker started")

	// Create resource cleanup service
	c.ResourceCleanupService = services.NewResourceCleanupService(
		c.NodeService,
		c.PeerService,
		c.IPService,
		f.logger,
	)
	f.logger.DebugContext(ctx, "resource cleanup service created successfully")

	// Create node rotation service
	c.NodeRotatorService = services.NewNodeRotationService(
		c.NodeService,
		c.PeerService,
		c.IPService,
		protoMgr,
		c.ResourceCleanupService,
		c.EventPublisher,
		f.logger,
	)
	f.logger.DebugContext(ctx, "node rotation service created successfully")

	// Create admin orchestrator (replaces previous admin service usage)
	c.AdminOrchestrator = orchestrator.NewAdminOrchestrator(
		c.NodeService,
		c.PeerService,
		c.IPService,
		c.ProvisioningService.GetProvisioningStateTracker(),
		f.logger,
	)
	f.logger.DebugContext(ctx, "admin orchestrator created successfully")

	// Create node orchestrator (only one that adds value beyond simple delegation)
	c.NodeOrchestrator = orchestrator.NewNodeOrchestrator(
		c.NodeRotatorService,
		c.NodeService,
		c.ProvisioningService.GetProvisioningStateTracker(),
		f.logger,
	)
	f.logger.DebugContext(ctx, "node orchestrator created successfully")

	// Create scheduler adapter
	c.SchedulerAdapter = orchestrator.NewSchedulerVPNService(
		c.NodeOrchestrator,
		c.ResourceCleanupService,
	)
	f.logger.DebugContext(ctx, "scheduler adapter created successfully")

	op.Complete("application services initialized successfully")
	return nil
}

func (f *ServiceFactory) createProvisioningService(c *ServiceComponents) error {
	ctx := context.Background()
	op := f.logger.StartOp(ctx, "create_provisioning_service")

	f.logger.DebugContext(ctx, "initializing provisioning service")

	provConfig := f.createProvisioningServiceConfig()
	provService := services.NewProvisioningService(
		c.NodeService,
		c.NodeRepository,
		c.CloudProvisioner,
		c.NodeInteractor,
		c.IPService,
		c.EventPublisher.Provisioning,
		provConfig,
		f.logger,
	)
	c.ProvisioningService = provService

	op.Complete("provisioning service initialized successfully")
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

func (f *ServiceFactory) createNodeInteractorConfig() remote.NodeInteractorConfig {
	// Use internal defaults for node interactor configuration
	nodeDefaults := f.internalDefaults.NodeInteractorDefaults()
	breakerDefaults := f.internalDefaults.CircuitBreakerDefaults()

	return remote.NodeInteractorConfig{
		SSHTimeout:          nodeDefaults.SSHTimeout,
		CommandTimeout:      nodeDefaults.CommandTimeout,
		WireGuardConfigPath: nodeDefaults.WireGuardConfigPath,
		WireGuardInterface:  nodeDefaults.WireGuardInterface,
		RetryAttempts:       nodeDefaults.RetryAttempts,
		RetryBackoff:        nodeDefaults.RetryBackoff,
		HealthCheckCommands: remote.DefaultConfig().HealthCheckCommands,
		CircuitBreaker: remote.CircuitBreakerConfig{
			FailureThreshold: breakerDefaults.FailureThreshold,
			ResetTimeout:     breakerDefaults.ResetTimeout,
			MaxAttempts:      breakerDefaults.MaxAttempts,
		},
	}
}

func (f *ServiceFactory) createProvisioningServiceConfig() services.ProvisioningServiceConfig {
	// Use centralized internal defaults for provisioning configuration
	eventDefaults := f.internalDefaults.EventSystemDefaults()
	nodeDefaults := f.internalDefaults.NodeInteractorDefaults()
	provDefaults := f.internalDefaults.ProvisioningDefaults()

	return services.ProvisioningServiceConfig{
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
