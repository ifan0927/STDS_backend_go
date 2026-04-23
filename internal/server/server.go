package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appnotification "stds_backend/internal/application/notification"
	appproperty "stds_backend/internal/application/property"
	"stds_backend/internal/config"
	"stds_backend/internal/http/router"
	"stds_backend/internal/platform/database"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
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
	propertyQueryRepo := dbpropertyquery.NewRepository(db)
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
	jobTriggerService := appjobs.NewTriggerService(jobRunStoreAdapter{repo: jobRunsRepo}, nil, cfg.App.SchedulerJobTimeout, cfg.App.SchedulerMaxRetries)

	engine := router.New(cfg.App, logger, db, authenticator, userRepo, router.AuthorizationRepositories{
		Properties:        propertyRepo,
		ResourceOwnership: resourceOwnershipRepo,
	}, createUserService, sendPasswordResetService, syncAuthService, updateCurrentUserService, updateUserService, assignUserPropertiesService, jobTriggerService, propertyQueryRepo, createPropertyService, updatePropertyService, deletePropertyService)

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
