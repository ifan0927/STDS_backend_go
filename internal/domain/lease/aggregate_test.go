package lease

import (
	"errors"
	"testing"
	"time"
)

func TestNewRejectsInvalidDateRange(t *testing.T) {
	_, err := New(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 6, 1),
		EndDate:                   date(2026, 5, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		DepositAmount:             36000,
	})
	if !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("expected ErrInvalidDateRange, got %v", err)
	}
}

func TestNewRejectsNonPositiveRent(t *testing.T) {
	_, err := New(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                0,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 5, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		DepositAmount:             36000,
	})
	if !errors.Is(err, ErrRentAmountNonPositive) {
		t.Fatalf("expected ErrRentAmountNonPositive, got %v", err)
	}
}

func TestNewRejectsNegativeDeposit(t *testing.T) {
	_, err := New(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 5, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		DepositAmount:             -1,
	})
	if !errors.Is(err, ErrDepositNegative) {
		t.Fatalf("expected ErrDepositNegative, got %v", err)
	}
}

func TestNewRejectsInvalidCadence(t *testing.T) {
	_, err := New(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 5, 31),
		ElectricityBillingCadence: "weekly",
		DepositAmount:             36000,
	})
	if !errors.Is(err, ErrInvalidCadence) {
		t.Fatalf("expected ErrInvalidCadence, got %v", err)
	}
}

func TestChangeRentUpdatesActiveLease(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	if err := aggregate.ChangeRent(20000); err != nil {
		t.Fatalf("ChangeRent: %v", err)
	}
	if aggregate.State().RentAmount != 20000 {
		t.Fatalf("RentAmount = %d, want 20000", aggregate.State().RentAmount)
	}
}

func TestChangeRentRejectsTerminatedLease(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusTerminated,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	if err := aggregate.ChangeRent(20000); !errors.Is(err, ErrLeaseNotActive) {
		t.Fatalf("expected ErrLeaseNotActive, got %v", err)
	}
}

func TestSettleDepositRecordsCompleteSettlement(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	reason := "cleaning"
	if err := aggregate.SettleDeposit(30000, 6000, &reason); err != nil {
		t.Fatalf("SettleDeposit: %v", err)
	}

	state := aggregate.State()
	if state.DepositStatus != DepositStatusSettled {
		t.Fatalf("DepositStatus = %q, want settled", state.DepositStatus)
	}
	if state.DepositRefundAmount == nil || *state.DepositRefundAmount != 30000 {
		t.Fatalf("DepositRefundAmount = %v, want 30000", state.DepositRefundAmount)
	}
	if state.DepositDeductionAmount == nil || *state.DepositDeductionAmount != 6000 {
		t.Fatalf("DepositDeductionAmount = %v, want 6000", state.DepositDeductionAmount)
	}
}

func TestSettleDepositRejectsDeductionWithoutReason(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	if err := aggregate.SettleDeposit(30000, 6000, nil); !errors.Is(err, ErrDepositReasonRequired) {
		t.Fatalf("expected ErrDepositReasonRequired, got %v", err)
	}

	blank := "   "
	if err := aggregate.SettleDeposit(30000, 6000, &blank); !errors.Is(err, ErrDepositReasonRequired) {
		t.Fatalf("expected ErrDepositReasonRequired for blank reason, got %v", err)
	}
}

func TestSettleDepositRejectsIncompleteSettlement(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	if err := aggregate.SettleDeposit(30000, 0, nil); !errors.Is(err, ErrDepositSettlementSum) {
		t.Fatalf("expected ErrDepositSettlementSum, got %v", err)
	}
}

func TestSettleDepositClearsReasonWhenThereIsNoDeduction(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusHeld,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	reason := "should be ignored"
	if err := aggregate.SettleDeposit(36000, 0, &reason); err != nil {
		t.Fatalf("SettleDeposit: %v", err)
	}
	if aggregate.State().DepositDeductionReason != nil {
		t.Fatalf("DepositDeductionReason = %q, want nil", *aggregate.State().DepositDeductionReason)
	}
}

func TestSettleDepositRejectsAlreadySettledDeposit(t *testing.T) {
	aggregate, err := Rehydrate(State{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                18000,
		StartDate:                 date(2026, 5, 1),
		EndDate:                   date(2026, 12, 31),
		ElectricityBillingCadence: BillingCadenceMonthly,
		Status:                    StatusActive,
		DepositAmount:             36000,
		DepositStatus:             DepositStatusSettled,
	})
	if err != nil {
		t.Fatalf("Rehydrate: %v", err)
	}

	if err := aggregate.SettleDeposit(36000, 0, nil); !errors.Is(err, ErrDepositNotHeld) {
		t.Fatalf("expected ErrDepositNotHeld, got %v", err)
	}
}

func TestBuildBillingPeriodsMonthlyDetailedCases(t *testing.T) {
	t.Run("general monthly periods stay anchored to start day", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2026, 5, 15), date(2026, 9, 20), BillingCadenceMonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2026, 5, 15), PeriodEnd: date(2026, 6, 14), DueDate: date(2026, 5, 15)},
			{PeriodStart: date(2026, 6, 15), PeriodEnd: date(2026, 7, 14), DueDate: date(2026, 6, 15)},
			{PeriodStart: date(2026, 7, 15), PeriodEnd: date(2026, 8, 14), DueDate: date(2026, 7, 15)},
			{PeriodStart: date(2026, 8, 15), PeriodEnd: date(2026, 9, 14), DueDate: date(2026, 8, 15)},
			{PeriodStart: date(2026, 9, 15), PeriodEnd: date(2026, 9, 20), DueDate: date(2026, 9, 15)},
		})
	})

	t.Run("month-end anchor clamps backward and keeps detailed boundaries", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2026, 1, 31), date(2026, 4, 29), BillingCadenceMonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2026, 1, 31), PeriodEnd: date(2026, 2, 27), DueDate: date(2026, 1, 31)},
			{PeriodStart: date(2026, 2, 28), PeriodEnd: date(2026, 3, 30), DueDate: date(2026, 2, 28)},
			{PeriodStart: date(2026, 3, 31), PeriodEnd: date(2026, 4, 29), DueDate: date(2026, 3, 31)},
		})
	})

	t.Run("leap year february uses month end for backward due date", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2028, 1, 31), date(2028, 3, 30), BillingCadenceMonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2028, 1, 31), PeriodEnd: date(2028, 2, 28), DueDate: date(2028, 1, 31)},
			{PeriodStart: date(2028, 2, 29), PeriodEnd: date(2028, 3, 30), DueDate: date(2028, 2, 29)},
		})
	})
}

func TestBuildBillingPeriodsNaturalBimonthlyDetailedCases(t *testing.T) {
	t.Run("odd month start uses remaining natural two-month window", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2026, 5, 15), date(2026, 10, 20), BillingCadenceBimonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2026, 5, 15), PeriodEnd: date(2026, 6, 30), DueDate: date(2026, 5, 15)},
			{PeriodStart: date(2026, 7, 1), PeriodEnd: date(2026, 8, 31), DueDate: date(2026, 7, 15)},
			{PeriodStart: date(2026, 9, 1), PeriodEnd: date(2026, 10, 20), DueDate: date(2026, 9, 15)},
		})
	})

	t.Run("even month start keeps natural window and clamps due date backward in current month", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2026, 6, 10), date(2026, 9, 25), BillingCadenceBimonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2026, 6, 10), PeriodEnd: date(2026, 6, 30), DueDate: date(2026, 6, 10)},
			{PeriodStart: date(2026, 7, 1), PeriodEnd: date(2026, 8, 31), DueDate: date(2026, 7, 10)},
			{PeriodStart: date(2026, 9, 1), PeriodEnd: date(2026, 9, 25), DueDate: date(2026, 9, 10)},
		})
	})

	t.Run("month-end anchor in bimonthly still uses backward due date in start month", func(t *testing.T) {
		periods, err := BuildBillingPeriods(date(2026, 6, 30), date(2026, 12, 5), BillingCadenceBimonthly)
		if err != nil {
			t.Fatalf("BuildBillingPeriods: %v", err)
		}

		assertPeriods(t, periods, []BillingPeriod{
			{PeriodStart: date(2026, 6, 30), PeriodEnd: date(2026, 6, 30), DueDate: date(2026, 6, 30)},
			{PeriodStart: date(2026, 7, 1), PeriodEnd: date(2026, 8, 31), DueDate: date(2026, 7, 30)},
			{PeriodStart: date(2026, 9, 1), PeriodEnd: date(2026, 10, 31), DueDate: date(2026, 9, 30)},
			{PeriodStart: date(2026, 11, 1), PeriodEnd: date(2026, 12, 5), DueDate: date(2026, 11, 30)},
		})
	})
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func assertPeriods(t *testing.T, got []BillingPeriod, want []BillingPeriod) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d periods, got %d", len(want), len(got))
	}

	for i := range want {
		if !got[i].PeriodStart.Equal(want[i].PeriodStart) || !got[i].PeriodEnd.Equal(want[i].PeriodEnd) || !got[i].DueDate.Equal(want[i].DueDate) {
			t.Fatalf("period %d mismatch: got %+v want %+v", i, got[i], want[i])
		}
	}
}
