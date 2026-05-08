package server

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	appbilling "stds_backend/internal/application/billing"
	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	applease "stds_backend/internal/application/lease"
	appproperty "stds_backend/internal/application/property"
	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/http/handler"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbleases "stds_backend/internal/platform/database/leases"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	dbusers "stds_backend/internal/platform/database/users"
	"stds_backend/internal/platform/eventbus"
)

func TestBuildJobRunnersRegistersLaunchJobKeys(t *testing.T) {
	runners := buildJobRunners(
		serverBillingJobRepositoryStub{},
		serverLeaseJobRepositoryStub{},
		serverForceTerminationJobRepositoryStub{},
		serverJobNotificationStub{},
		serverJobTxRunnerStub{},
	)

	expected := []appjobs.JobKey{
		appjobs.JobOverdueBillsScan,
		appjobs.JobOverdueBillReminders,
		appjobs.JobLeaseExpiryScan,
		appjobs.JobLeaseExpiringSoonReminder,
		appjobs.JobMonthlySnapshot,
		appjobs.JobForceTerminationCompensation,
	}

	if len(runners) != len(expected) {
		t.Fatalf("expected %d runners, got %d", len(expected), len(runners))
	}
	for _, key := range expected {
		if runners[key] == nil {
			t.Fatalf("expected runner for %q", key)
		}
	}
}

func TestRegisterLeaseEventSubscribersPublishesToLifecycleHandlers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &recordingLeaseSubscriberRepository{}
	bus := eventbus.New()
	txRunner := dbtxrunner.New(db, nil)
	registerLeaseEventSubscribers(bus, repo, txRunner)

	created := domainevents.LeaseCreated{
		LeaseID:    "lease-1",
		RoomID:     "room-1",
		PropertyID: "property-1",
		TenantID:   "tenant-1",
		OccurredAt: time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC),
	}
	if err := bus.Publish(context.Background(), created); err != nil {
		t.Fatalf("publish LeaseCreated: %v", err)
	}

	terminated := domainevents.LeaseTerminated{
		LeaseID:    "lease-1",
		RoomID:     "room-1",
		PropertyID: "property-1",
		TenantID:   "tenant-1",
		OccurredAt: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
	}
	if err := bus.Publish(context.Background(), terminated); err != nil {
		t.Fatalf("publish LeaseTerminated: %v", err)
	}

	if !reflect.DeepEqual(repo.occupiedRooms, []string{"room-1"}) {
		t.Fatalf("occupied rooms = %+v", repo.occupiedRooms)
	}
	if !reflect.DeepEqual(repo.activatedTenants, []string{"tenant-1"}) {
		t.Fatalf("activated tenants = %+v", repo.activatedTenants)
	}
	if !reflect.DeepEqual(repo.vacantRooms, []string{"room-1"}) {
		t.Fatalf("vacant rooms = %+v", repo.vacantRooms)
	}
	if !reflect.DeepEqual(repo.deactivatedTenants, []string{"tenant-1"}) {
		t.Fatalf("deactivated tenants = %+v", repo.deactivatedTenants)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestIAMUserMappingsPreserveContractFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	overrides := []map[string]interface{}{{"action": "read", "resource": "billing"}}
	properties := []string{"property-1", "property-2"}
	user := &dbusers.User{
		ID:                  "user-1",
		FirebaseUID:         "firebase-1",
		Email:               "user@example.com",
		Name:                "User One",
		Role:                "staff",
		PermissionOverrides: overrides,
		AssignedPropertyIDs: properties,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		Version:             7,
	}

	expectedAccount := &appiam.UserAccount{
		ID:                  "user-1",
		FirebaseUID:         "firebase-1",
		Email:               "user@example.com",
		Name:                "User One",
		Role:                "staff",
		PermissionOverrides: overrides,
		AssignedPropertyIDs: properties,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		Version:             7,
	}
	if got := toUserAccount(user); !reflect.DeepEqual(got, expectedAccount) {
		t.Fatalf("toUserAccount = %+v, want %+v", got, expectedAccount)
	}

	expectedManaged := &appiam.ManagedUser{
		ID:                  "user-1",
		FirebaseUID:         "firebase-1",
		Email:               "user@example.com",
		Name:                "User One",
		Role:                "staff",
		PermissionOverrides: overrides,
		AssignedPropertyIDs: properties,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		Version:             7,
	}
	if got := toManagedUser(user); !reflect.DeepEqual(got, expectedManaged) {
		t.Fatalf("toManagedUser = %+v, want %+v", got, expectedManaged)
	}
}

func TestPropertyMappingsPreserveContractFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	unitPrice := 4.25
	status := "in_progress"
	assignedTo := "staff-1"
	assignedAt := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)
	completedAt := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)

	property := toApplicationProperty(&dbproperties.Property{
		ID:                               "property-1",
		Name:                             "Property One",
		Address:                          "Address One",
		ElectricityUnitPrice:             &unitPrice,
		DefaultElectricityBillingCadence: "bimonthly",
		OwnerID:                          "owner-1",
		CreatedAt:                        createdAt,
		UpdatedAt:                        updatedAt,
		Version:                          3,
	})
	expectedProperty := &appproperty.Property{
		ID:                               "property-1",
		Name:                             "Property One",
		Address:                          "Address One",
		ElectricityUnitPrice:             &unitPrice,
		DefaultElectricityBillingCadence: "bimonthly",
		OwnerID:                          "owner-1",
		CreatedAt:                        createdAt,
		UpdatedAt:                        updatedAt,
		Version:                          3,
	}
	if !reflect.DeepEqual(property, expectedProperty) {
		t.Fatalf("toApplicationProperty = %+v, want %+v", property, expectedProperty)
	}

	room := toApplicationRoom(&dbproperties.Room{
		ID:         "room-1",
		PropertyID: "property-1",
		Name:       "Room A",
		Status:     "vacant",
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	})
	expectedRoom := &appproperty.Room{
		ID:         "room-1",
		PropertyID: "property-1",
		Name:       "Room A",
		Status:     "vacant",
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}
	if !reflect.DeepEqual(room, expectedRoom) {
		t.Fatalf("toApplicationRoom = %+v, want %+v", room, expectedRoom)
	}

	repair := toApplicationRepairRequest(&dbproperties.RepairRequest{
		ID:          "repair-1",
		PropertyID:  "property-1",
		RoomID:      "room-1",
		SubmittedBy: "tenant-1",
		AssignedTo:  &assignedTo,
		Title:       "Leaking faucet",
		Description: "Kitchen sink",
		Status:      status,
		SubmittedAt: createdAt,
		AssignedAt:  &assignedAt,
		CompletedAt: &completedAt,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	})
	expectedRepair := &appproperty.RepairRequest{
		ID:          "repair-1",
		PropertyID:  "property-1",
		RoomID:      "room-1",
		SubmittedBy: "tenant-1",
		AssignedTo:  &assignedTo,
		Title:       "Leaking faucet",
		Description: "Kitchen sink",
		Status:      status,
		SubmittedAt: createdAt,
		AssignedAt:  &assignedAt,
		CompletedAt: &completedAt,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	if !reflect.DeepEqual(repair, expectedRepair) {
		t.Fatalf("toApplicationRepairRequest = %+v, want %+v", repair, expectedRepair)
	}
}

func TestLeaseMappingsPreserveContractFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	refundAmount := 1000
	deductionAmount := 500
	deductionReason := "cleaning"
	notes := "renewal"
	terminationReason := "tenant_request"
	settlementDetail := map[string]interface{}{"refund": float64(1000)}
	startingMeterReading := 1250

	lease := toApplicationLease(&dbleases.Lease{
		ID:                        "lease-1",
		TenantID:                  "tenant-1",
		PropertyID:                "property-1",
		RoomID:                    "room-1",
		RentAmount:                18000,
		StartDate:                 startDate,
		EndDate:                   endDate,
		ElectricityBillingCadence: "monthly",
		StartingMeterReading:      &startingMeterReading,
		Status:                    "terminated",
		DepositAmount:             36000,
		DepositRefundAmount:       &refundAmount,
		DepositDeductionAmount:    &deductionAmount,
		DepositStatus:             "settled",
		DepositDeductionReason:    &deductionReason,
		Notes:                     &notes,
		TerminationReason:         &terminationReason,
		SettlementDetail:          &settlementDetail,
		CreatedAt:                 createdAt,
		UpdatedAt:                 updatedAt,
		Version:                   8,
	})
	expectedLease := &applease.Lease{
		ID:                        "lease-1",
		TenantID:                  "tenant-1",
		PropertyID:                "property-1",
		RoomID:                    "room-1",
		RentAmount:                18000,
		StartDate:                 startDate,
		EndDate:                   endDate,
		ElectricityBillingCadence: "monthly",
		StartingMeterReading:      &startingMeterReading,
		Status:                    "terminated",
		DepositAmount:             36000,
		DepositRefundAmount:       &refundAmount,
		DepositDeductionAmount:    &deductionAmount,
		DepositStatus:             "settled",
		DepositDeductionReason:    &deductionReason,
		Notes:                     &notes,
		TerminationReason:         &terminationReason,
		SettlementDetail:          &settlementDetail,
		CreatedAt:                 createdAt,
		UpdatedAt:                 updatedAt,
		Version:                   8,
	}
	if !reflect.DeepEqual(lease, expectedLease) {
		t.Fatalf("toApplicationLease = %+v, want %+v", lease, expectedLease)
	}

	forceTermination := toApplicationForceTermination(&dbleases.ForceTermination{
		ID:               "force-1",
		LeaseID:          "lease-1",
		PropertyID:       "property-1",
		RoomID:           "room-1",
		TenantID:         "tenant-1",
		PropertyLabel:    "Property A",
		RoomLabel:        "Room 101",
		TenantLabel:      "Tenant A",
		InitiatedByLabel: "Admin A",
		Status:           "bills_done",
		InitiatedBy:      "admin-1",
		Reason:           "legacy cleanup",
		DepositHandling:  "write_off",
		Bills: []dbleases.ForceTerminationBill{
			{BillID: "bill-1", Status: "done", Type: "rent", PeriodStart: startDate, PeriodEnd: endDate, PeriodLabel: "2026-05-15..2026-12-31"},
			{BillID: "bill-2", Status: "pending", Type: "electricity", PeriodStart: startDate, PeriodEnd: endDate, PeriodLabel: "2026-05-15..2026-12-31"},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})
	expectedForceTermination := &applease.ForceTermination{
		ID:               "force-1",
		LeaseID:          "lease-1",
		PropertyID:       "property-1",
		RoomID:           "room-1",
		TenantID:         "tenant-1",
		PropertyLabel:    "Property A",
		RoomLabel:        "Room 101",
		TenantLabel:      "Tenant A",
		InitiatedByLabel: "Admin A",
		Status:           "bills_done",
		InitiatedBy:      "admin-1",
		Reason:           "legacy cleanup",
		DepositHandling:  "write_off",
		Bills: []applease.ForceTerminationBill{
			{BillID: "bill-1", Status: "done", Type: "rent", PeriodStart: startDate, PeriodEnd: endDate, PeriodLabel: "2026-05-15..2026-12-31"},
			{BillID: "bill-2", Status: "pending", Type: "electricity", PeriodStart: startDate, PeriodEnd: endDate, PeriodLabel: "2026-05-15..2026-12-31"},
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if !reflect.DeepEqual(forceTermination, expectedForceTermination) {
		t.Fatalf("toApplicationForceTermination = %+v, want %+v", forceTermination, expectedForceTermination)
	}
}

func TestBillingMappingsPreserveContractFields(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	dueDate := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	paidAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	meterRecordedAt := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	amount := 18000
	paidAmount := 18000
	previousReading := 100
	currentReading := 130
	unitPrice := 4.5
	paymentMethod := "cash"
	writtenOffReason := "force termination"

	dbBill := dbbilling.Bill{
		ID:                   "bill-1",
		LeaseID:              "lease-1",
		TenantID:             "tenant-1",
		RoomID:               "room-1",
		PropertyID:           "property-1",
		Type:                 "electricity",
		Amount:               &amount,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		DueDate:              dueDate,
		Status:               "paid",
		PaymentMethod:        &paymentMethod,
		PaidAt:               &paidAt,
		PaidAmount:           &paidAmount,
		MeterPreviousReading: &previousReading,
		MeterCurrentReading:  &currentReading,
		MeterUnitPrice:       &unitPrice,
		MeterRecordedAt:      &meterRecordedAt,
		WrittenOffReason:     &writtenOffReason,
		OverdueNoticeCount:   2,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		Version:              5,
	}

	expectedAppBill := &appbilling.Bill{
		ID:                   "bill-1",
		LeaseID:              "lease-1",
		TenantID:             "tenant-1",
		RoomID:               "room-1",
		PropertyID:           "property-1",
		Type:                 "electricity",
		Amount:               &amount,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		DueDate:              dueDate,
		Status:               "paid",
		PaymentMethod:        &paymentMethod,
		PaidAt:               &paidAt,
		PaidAmount:           &paidAmount,
		MeterPreviousReading: &previousReading,
		MeterCurrentReading:  &currentReading,
		MeterUnitPrice:       &unitPrice,
		MeterRecordedAt:      &meterRecordedAt,
		WrittenOffReason:     &writtenOffReason,
		OverdueNoticeCount:   2,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		Version:              5,
	}
	if got := toAppBill(dbBill); !reflect.DeepEqual(got, expectedAppBill) {
		t.Fatalf("toAppBill = %+v, want %+v", got, expectedAppBill)
	}

	expectedHandlerBill := &handler.BillingBill{
		ID:                   "bill-1",
		LeaseID:              "lease-1",
		TenantID:             "tenant-1",
		RoomID:               "room-1",
		PropertyID:           "property-1",
		Type:                 "electricity",
		Amount:               &amount,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		DueDate:              dueDate,
		Status:               "paid",
		PaymentMethod:        &paymentMethod,
		PaidAt:               &paidAt,
		PaidAmount:           &paidAmount,
		MeterPreviousReading: &previousReading,
		MeterCurrentReading:  &currentReading,
		MeterUnitPrice:       &unitPrice,
		MeterRecordedAt:      &meterRecordedAt,
		WrittenOffReason:     &writtenOffReason,
		OverdueNoticeCount:   2,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		Version:              5,
	}
	if got := toHandlerBill(*expectedAppBill); !reflect.DeepEqual(got, expectedHandlerBill) {
		t.Fatalf("toHandlerBill = %+v, want %+v", got, expectedHandlerBill)
	}
}

type recordingLeaseSubscriberRepository struct {
	applease.Repository
	occupiedRooms      []string
	vacantRooms        []string
	activatedTenants   []string
	deactivatedTenants []string
}

func (r *recordingLeaseSubscriberRepository) MarkRoomOccupied(_ context.Context, _ *sql.Tx, roomID string) error {
	r.occupiedRooms = append(r.occupiedRooms, roomID)
	return nil
}

func (r *recordingLeaseSubscriberRepository) MarkRoomVacant(_ context.Context, _ *sql.Tx, roomID string) error {
	r.vacantRooms = append(r.vacantRooms, roomID)
	return nil
}

func (r *recordingLeaseSubscriberRepository) ActivateTenant(_ context.Context, _ *sql.Tx, tenantID string) error {
	r.activatedTenants = append(r.activatedTenants, tenantID)
	return nil
}

func (r *recordingLeaseSubscriberRepository) DeactivateTenantIfNoActiveLeases(_ context.Context, _ *sql.Tx, tenantID string) error {
	r.deactivatedTenants = append(r.deactivatedTenants, tenantID)
	return nil
}

type serverBillingJobRepositoryStub struct{}

func (serverBillingJobRepositoryStub) ListOverdueScanCandidates(context.Context, time.Time) ([]appjobs.BillCandidate, error) {
	return nil, nil
}
func (serverBillingJobRepositoryStub) MarkBillOverdue(context.Context, *sql.Tx, string, int) error {
	return nil
}
func (serverBillingJobRepositoryStub) ListOverdueReminderCandidates(context.Context) ([]appjobs.OverdueReminderCandidate, error) {
	return nil, nil
}
func (serverBillingJobRepositoryStub) IncrementOverdueNoticeCount(context.Context, *sql.Tx, string, int) error {
	return nil
}
func (serverBillingJobRepositoryStub) ListMonthlySnapshotProperties(context.Context) ([]appjobs.MonthlySnapshotProperty, error) {
	return nil, nil
}
func (serverBillingJobRepositoryStub) MonthlySnapshotExists(context.Context, *sql.Tx, string, int, int) (bool, error) {
	return false, nil
}
func (serverBillingJobRepositoryStub) CreateMonthlySnapshot(context.Context, *sql.Tx, string, int, int) error {
	return nil
}

type serverLeaseJobRepositoryStub struct{}

func (serverLeaseJobRepositoryStub) ListLeaseExpiryCandidates(context.Context, time.Time) ([]appjobs.LeaseCandidate, error) {
	return nil, nil
}
func (serverLeaseJobRepositoryStub) MarkLeaseExpired(context.Context, *sql.Tx, string, int) error {
	return nil
}
func (serverLeaseJobRepositoryStub) ListLeaseExpiringSoonCandidates(context.Context, time.Time) ([]appjobs.LeaseExpiringSoonCandidate, error) {
	return nil, nil
}
func (serverLeaseJobRepositoryStub) ListLeaseExpiringSoonRecipients(context.Context, string) ([]appjobs.NotificationRecipient, error) {
	return nil, nil
}

type serverForceTerminationJobRepositoryStub struct{}

func (serverForceTerminationJobRepositoryStub) ListInProgressForceTerminations(context.Context) ([]appjobs.ForceTerminationCandidate, error) {
	return nil, nil
}
func (serverForceTerminationJobRepositoryStub) ListPendingForceTerminationBillIDs(context.Context, *sql.Tx, string) ([]string, error) {
	return nil, nil
}
func (serverForceTerminationJobRepositoryStub) WriteOffBills(context.Context, *sql.Tx, []string, string) (int, error) {
	return 0, nil
}
func (serverForceTerminationJobRepositoryStub) MarkForceTerminationBillsDone(context.Context, *sql.Tx, string, []string) error {
	return nil
}
func (serverForceTerminationJobRepositoryStub) CompleteForceTermination(context.Context, *sql.Tx, string) error {
	return nil
}

type serverJobNotificationStub struct{}

func (serverJobNotificationStub) SendOverdueBillReminder(context.Context, appjobs.NotificationRecipient, appjobs.OverdueReminderCandidate) error {
	return nil
}
func (serverJobNotificationStub) SendLeaseExpiringSoon(context.Context, appjobs.NotificationRecipient, appjobs.LeaseExpiringSoonCandidate) error {
	return nil
}

type serverJobTxRunnerStub struct{}

func (serverJobTxRunnerStub) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *dbtxrunner.EventRecorder) error) error {
	return fn(ctx, nil, nil)
}
