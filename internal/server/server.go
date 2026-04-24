package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	applease "stds_backend/internal/application/lease"
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	apptenant "stds_backend/internal/application/tenant"
	"stds_backend/internal/config"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/router"
	"stds_backend/internal/platform/database"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbleases "stds_backend/internal/platform/database/leases"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtenantquery "stds_backend/internal/platform/database/tenantquery"
	dbtenants "stds_backend/internal/platform/database/tenants"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	dbusers "stds_backend/internal/platform/database/users"
	"stds_backend/internal/platform/eventbus"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/platform/logging"
	platformnotification "stds_backend/internal/platform/notification"
)

// Server owns the application's HTTP server and process-level dependencies.
type Server struct {
	httpServer *http.Server
	db         *sql.DB
	logger     *slog.Logger
}

// New wires configuration, infrastructure clients, and the HTTP router into a
// runnable server instance.
func New(cfg *config.Config) (*Server, error) {
	db, err := database.Open(context.Background(), cfg.DB.URL)
	if err != nil {
		return nil, err
	}

	logger := logging.New(cfg.App)

	authenticator, err := platformfirebase.New(context.Background(), cfg.Firebase)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	userRepo := dbusers.NewRepository(db)
	propertyRepo := dbproperties.NewRepository(db)
	leaseRepo := dbleases.NewRepository(db)
	propertyQueryRepo := dbpropertyquery.NewRepository(db)
	leaseQueryRepo := dbleasequery.NewRepository(db)
	tenantRepo := dbtenants.NewRepository(db)
	tenantQueryRepo := dbtenantquery.NewRepository(db)
	resourceOwnershipRepo := dbresourceownership.NewRepository(db)
	jobRunsRepo := dbjobruns.NewRepository(db)

	emailSender, err := platformnotification.NewResendSender(cfg.Notify)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	dispatcher := platformnotification.NewDispatcher(emailSender, platformnotification.StubLineSender{})
	notificationService := appnotification.NewService(dispatcher)
	bus := eventbus.New()
	eventbus.Subscribe(bus, notificationService.HandleUserPasswordResetRequested)

	userAccountRepo := userAccountRepositoryAdapter{repo: userRepo}
	createUserService := appiam.NewCreateUserService(userAccountRepo, authenticator, notificationService)
	sendPasswordResetService := appiam.NewSendUserPasswordResetService(userAccountRepo, authenticator, notificationService)
	customClaimsService := appiam.NewCustomClaimsService(authenticator)
	syncAuthService := appiam.NewSyncAuthService(userAccountRepo, customClaimsService)
	updateCurrentUserService := appiam.NewUpdateCurrentUserService(userRepo)
	updateUserService := appiam.NewUpdateUserService(userAccountRepo, customClaimsService)
	assignUserPropertiesService := appiam.NewAssignUserPropertiesService(
		managedUserRepositoryAdapter{repo: userRepo},
		propertyExistenceCheckerAdapter{repo: propertyQueryRepo},
		customClaimsService,
	)
	txRunner := dbtxrunner.New(db, bus)
	createPropertyService := appproperty.NewCreatePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	updatePropertyService := appproperty.NewUpdatePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	deletePropertyService := appproperty.NewDeletePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	createRoomService := appproperty.NewCreateRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	updateRoomService := appproperty.NewUpdateRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	deleteRoomService := appproperty.NewDeleteRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	setRoomMaintenanceService := appproperty.NewSetRoomMaintenanceService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	createTenantService := apptenant.NewCreateTenantService(tenantRepositoryAdapter{repo: tenantRepo}, txRunner)
	updateTenantService := apptenant.NewUpdateTenantService(tenantRepositoryAdapter{repo: tenantRepo}, txRunner)
	createLeaseService := applease.NewCreateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	updateLeaseService := applease.NewUpdateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	updateDepositService := applease.NewUpdateDepositService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	replaceLeaseService := applease.NewReplaceLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	terminateLeaseService := applease.NewTerminateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	forceTerminateLeaseService := applease.NewForceTerminateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	getForceTerminationService := applease.NewGetForceTerminationService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	occupyRoomOnLeaseCreated := applease.NewOccupyRoomOnLeaseCreatedHandler(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	activateTenantOnLeaseCreated := applease.NewActivateTenantOnLeaseCreatedHandler(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	releaseRoomOnLeaseTerminated := applease.NewReleaseRoomOnLeaseTerminatedHandler(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	deactivateTenantOnLeaseTerminated := applease.NewDeactivateTenantOnLeaseTerminatedHandler(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	jobTriggerService := appjobs.NewTriggerService(jobRunStoreAdapter{repo: jobRunsRepo}, nil, cfg.App.SchedulerJobTimeout, cfg.App.SchedulerMaxRetries)
	eventbus.Subscribe(bus, occupyRoomOnLeaseCreated.HandleLeaseCreated)
	eventbus.Subscribe(bus, activateTenantOnLeaseCreated.HandleLeaseCreated)
	eventbus.Subscribe(bus, releaseRoomOnLeaseTerminated.HandleLeaseTerminated)
	eventbus.Subscribe(bus, deactivateTenantOnLeaseTerminated.HandleLeaseTerminated)

	engine := router.New(cfg.App, logger, db, authenticator, userRepo, router.AuthorizationRepositories{
		Properties:        propertyRepo,
		ResourceOwnership: resourceOwnershipRepo,
	}, createUserService, sendPasswordResetService, syncAuthService, updateCurrentUserService, updateUserService, assignUserPropertiesService, jobTriggerService, propertyQueryRepo, leaseQueryRepo, tenantQueryRepo, createPropertyService, updatePropertyService, deletePropertyService, createRoomService, updateRoomService, deleteRoomService, setRoomMaintenanceService, createTenantService, updateTenantService, createLeaseService, handler.LeaseCommandServices{
		UpdateLease:         updateLeaseService,
		UpdateDeposit:       updateDepositService,
		ReplaceLease:        replaceLeaseService,
		TerminateLease:      terminateLeaseService,
		ForceTerminateLease: forceTerminateLeaseService,
		GetForceTermination: getForceTerminationService,
		Billing:             newBillingServices(db, txRunner),
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.App.Address(),
			Handler:      engine,
			ReadTimeout:  cfg.App.ReadTimeout,
			WriteTimeout: cfg.App.WriteTimeout,
		},
		db:     db,
		logger: logger,
	}, nil
}

// Run starts the HTTP server and blocks until it exits.
func (s *Server) Run() error {
	defer s.db.Close()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}

	return nil
}
