package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	appattachment "stds_backend/internal/application/attachment"
	appbrand "stds_backend/internal/application/brand"
	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appjournal "stds_backend/internal/application/journal"
	applease "stds_backend/internal/application/lease"
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	apprepair "stds_backend/internal/application/repair"
	apptenant "stds_backend/internal/application/tenant"
	"stds_backend/internal/config"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/router"
	"stds_backend/internal/platform/database"
	dbattachments "stds_backend/internal/platform/database/attachments"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbbrandfaq "stds_backend/internal/platform/database/brandfaq"
	dbbrandprofile "stds_backend/internal/platform/database/brandprofile"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbjournal "stds_backend/internal/platform/database/journal"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbleases "stds_backend/internal/platform/database/leases"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertydashboard "stds_backend/internal/platform/database/propertydashboard"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbrepair "stds_backend/internal/platform/database/repair"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtenantquery "stds_backend/internal/platform/database/tenantquery"
	dbtenants "stds_backend/internal/platform/database/tenants"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	dbusers "stds_backend/internal/platform/database/users"
	"stds_backend/internal/platform/eventbus"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/platform/logging"
	platformnotification "stds_backend/internal/platform/notification"
	platformstorage "stds_backend/internal/platform/storage"
)

// Server owns the application's HTTP server and process-level dependencies.
type Server struct {
	httpServer *http.Server
	db         *sql.DB
	logger     *slog.Logger
	storage    platformstorage.AttachmentStorage
}

// New wires configuration, infrastructure clients, and the HTTP router into a
// runnable server instance.
func New(cfg *config.Config) (*Server, error) {
	db, err := database.Open(context.Background(), cfg.DB.URL, database.PoolConfig{
		MaxOpenConns:    cfg.DB.MaxOpenConns,
		MaxIdleConns:    cfg.DB.MaxIdleConns,
		ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
	})
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
	billingRepo := dbbilling.NewRepository(db)
	leaseRepo := dbleases.NewRepository(db)
	propertyDashboardRepo := dbpropertydashboard.NewRepository(db)
	propertyQueryRepo := dbpropertyquery.NewRepository(db)
	leaseQueryRepo := dbleasequery.NewRepository(db)
	journalRepo := dbjournal.NewRepository(db)
	repairRepo := dbrepair.NewRepository(db)
	tenantRepo := dbtenants.NewRepository(db)
	tenantQueryRepo := dbtenantquery.NewRepository(db)
	resourceOwnershipRepo := dbresourceownership.NewRepository(db)
	jobRunsRepo := dbjobruns.NewRepository(db)
	attachmentRepo := dbattachments.NewRepository(db)
	brandProfileRepo := dbbrandprofile.NewRepository(db)
	brandFAQRepo := dbbrandfaq.NewRepository(db)

	storageClient, err := platformstorage.NewAttachmentStorage(context.Background(), cfg.App.Env, cfg.Storage)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	emailSender, err := platformnotification.NewResendSender(cfg.Notify)
	if err != nil {
		_ = storageClient.Close()
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
	brandProfileService := appbrand.NewService(brandProfileRepositoryAdapter{repo: brandProfileRepo}, txRunner)
	brandFAQService := appbrand.NewFAQService(brandFAQRepositoryAdapter{repo: brandFAQRepo}, txRunner)
	createPropertyService := appproperty.NewCreatePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, propertyAccountRepositoryAdapter{repo: billingRepo}, txRunner)
	updatePropertyService := appproperty.NewUpdatePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	deletePropertyService := appproperty.NewDeletePropertyService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	createRoomService := appproperty.NewCreateRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	updateRoomService := appproperty.NewUpdateRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	deleteRoomService := appproperty.NewDeleteRoomService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	setRoomMaintenanceService := appproperty.NewSetRoomMaintenanceService(propertyRepositoryAdapter{repo: propertyRepo}, txRunner)
	propertyDashboardService := appproperty.NewDashboardService(propertyDashboardRepo)
	createTenantService := apptenant.NewCreateTenantService(tenantRepositoryAdapter{repo: tenantRepo}, txRunner)
	updateTenantService := apptenant.NewUpdateTenantService(tenantRepositoryAdapter{repo: tenantRepo}, txRunner)
	createLeaseService := applease.NewCreateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	updateLeaseService := applease.NewUpdateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	leaseDepositAccounting := leaseDepositAccountingAdapter{repo: billingRepo}
	updateDepositService := applease.NewUpdateDepositService(leaseRepositoryAdapter{repo: leaseRepo}, leaseDepositAccounting, txRunner)
	replaceLeaseService := applease.NewReplaceLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	terminateLeaseService := applease.NewTerminateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, leaseDepositAccounting, txRunner)
	previewCheckoutService := applease.NewPreviewCheckoutSettlementService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	finalizeCheckoutService := applease.NewFinalizeCheckoutSettlementService(leaseRepositoryAdapter{repo: leaseRepo}, leaseDepositAccounting, txRunner)
	exportCheckoutService := applease.NewExportCheckoutSettlementService(leaseRepositoryAdapter{repo: leaseRepo}, applease.MustNewCheckoutSettlementRenderer(), txRunner)
	forceTerminateLeaseService := applease.NewForceTerminateLeaseService(leaseRepositoryAdapter{repo: leaseRepo}, leaseDepositAccounting, txRunner)
	getForceTerminationService := applease.NewGetForceTerminationService(leaseRepositoryAdapter{repo: leaseRepo}, txRunner)
	attachmentService := appattachment.NewService(
		attachmentRepositoryAdapter{repo: attachmentRepo},
		storageClient,
		attachmentResourceAccessAdapter{ownership: resourceOwnershipRepo},
		txRunner,
		cfg.Storage.GCSSignedURLTTL,
	)
	repairServices := handler.RepairServices{
		Create:   apprepair.NewCreateService(repairRepo, txRunner),
		Update:   apprepair.NewUpdateService(repairRepo, txRunner),
		Delete:   apprepair.NewDeleteService(repairRepo, txRunner),
		Workflow: apprepair.NewWorkflowService(repairRepo, txRunner),
	}
	journalAccounting := journalExpenseAccountingAdapter{repo: billingRepo}
	journalServices := handler.JournalServices{
		List:                     appjournal.NewListService(journalRepo),
		Get:                      appjournal.NewGetService(journalRepo),
		ListExpenseAccountTitles: appjournal.NewListExpenseAccountingTitlesService(journalRepo),
		Create:                   appjournal.NewCreateService(journalRepo, journalAccounting, txRunner),
		Update:                   appjournal.NewUpdateService(journalRepo, journalAccounting, txRunner),
		Delete:                   appjournal.NewDeleteService(journalRepo, journalAccounting, txRunner),
	}
	jobTriggerService := appjobs.NewTriggerService(jobRunStoreAdapter{repo: jobRunsRepo}, newJobRunners(db, txRunner, notificationService), cfg.App.SchedulerJobTimeout, cfg.App.SchedulerMaxRetries)
	registerLeaseEventSubscribers(bus, leaseRepositoryAdapter{repo: leaseRepo}, txRunner)

	engine := router.New(cfg.App, logger, db, authenticator, userRepo, router.AuthorizationRepositories{
		Properties:        propertyRepo,
		ResourceOwnership: resourceOwnershipRepo,
	}, handler.APIServerDeps{
		UserRepo:          userRepo,
		CreateUser:        createUserService,
		SendPasswordReset: sendPasswordResetService,
		SyncAuth:          syncAuthService,
		UpdateCurrentUser: updateCurrentUserService,
		UpdateUser:        updateUserService,
		AssignProperties:  assignUserPropertiesService,
		BrandProfile:      brandProfileService,
		BrandFAQ:          brandFAQService,
		JobTrigger:        jobTriggerService,
		PropertyQuery:     propertyQueryRepo,
		PropertyDashboard: propertyDashboardService,
		LeaseQuery:        leaseQueryRepo,
		RepairQuery:       repairRepo,
		TenantQuery:       tenantQueryRepo,
		CreateProperty:    createPropertyService,
		UpdateProperty:    updatePropertyService,
		DeleteProperty:    deletePropertyService,
		CreateRoom:        createRoomService,
		UpdateRoom:        updateRoomService,
		DeleteRoom:        deleteRoomService,
		SetMaintenance:    setRoomMaintenanceService,
		CreateTenant:      createTenantService,
		UpdateTenant:      updateTenantService,
		CreateLease:       createLeaseService,
		Leases: handler.LeaseServices{
			UpdateLease:         updateLeaseService,
			UpdateDeposit:       updateDepositService,
			ReplaceLease:        replaceLeaseService,
			TerminateLease:      terminateLeaseService,
			PreviewCheckout:     previewCheckoutService,
			FinalizeCheckout:    finalizeCheckoutService,
			ExportCheckout:      exportCheckoutService,
			ForceTerminateLease: forceTerminateLeaseService,
			GetForceTermination: getForceTerminationService,
		},
		Billing:    newBillingServices(db, txRunner, bus),
		Journal:    journalServices,
		Repair:     repairServices,
		Attachment: attachmentService,
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.App.Address(),
			Handler:      engine,
			ReadTimeout:  cfg.App.ReadTimeout,
			WriteTimeout: cfg.App.WriteTimeout,
		},
		db:      db,
		logger:  logger,
		storage: storageClient,
	}, nil
}

func registerLeaseEventSubscribers(bus *eventbus.Bus, repo applease.Repository, txRunner *dbtxrunner.Runner) {
	occupyRoomOnLeaseCreated := applease.NewOccupyRoomOnLeaseCreatedHandler(repo, txRunner)
	activateTenantOnLeaseCreated := applease.NewActivateTenantOnLeaseCreatedHandler(repo, txRunner)
	releaseRoomOnLeaseTerminated := applease.NewReleaseRoomOnLeaseTerminatedHandler(repo, txRunner)
	deactivateTenantOnLeaseTerminated := applease.NewDeactivateTenantOnLeaseTerminatedHandler(repo, txRunner)

	eventbus.Subscribe(bus, occupyRoomOnLeaseCreated.HandleLeaseCreated)
	eventbus.Subscribe(bus, activateTenantOnLeaseCreated.HandleLeaseCreated)
	eventbus.Subscribe(bus, releaseRoomOnLeaseTerminated.HandleLeaseTerminated)
	eventbus.Subscribe(bus, deactivateTenantOnLeaseTerminated.HandleLeaseTerminated)
}

// Run starts the HTTP server and blocks until it exits.
func (s *Server) Run() error {
	defer s.db.Close()
	defer s.storage.Close()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}

	return nil
}
