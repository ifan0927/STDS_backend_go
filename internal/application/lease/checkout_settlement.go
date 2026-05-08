package lease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
	"stds_backend/internal/shared/reporthtml"
)

const (
	checkoutLineDepositRefund       = "deposit_refund"
	checkoutLineCleaningFee         = "cleaning_fee"
	checkoutLineKeyCardLoss         = "key_card_loss"
	checkoutLineOtherFee            = "other_fee"
	checkoutLineElectricSettlement  = "electricity_settlement"
	checkoutLineRentRefund          = "rent_refund"
	checkoutDirectionRefund         = "refund"
	checkoutDirectionCharge         = "charge"
	checkoutDirectionInfo           = "info"
	checkoutNetRefund               = "refund"
	checkoutNetPayable              = "payable"
	checkoutNetZero                 = "zero"
	checkoutWarningRentRefund       = "rent_refund_not_calculated"
	checkoutWarningFinalMeter       = "final_meter_snapshot_only"
	checkoutBlockerUnpaidBill       = "unpaid_bill"
	checkoutBlockerPendingMeter     = "pending_meter"
	checkoutBlockerLeaseNotActive   = "lease_not_active"
	checkoutBlockerDepositNotHeld   = "deposit_not_held"
	checkoutBlockerChargeExceedsDep = "charge_exceeds_deposit"
	checkoutBlockerRentRefund       = "rent_refund_not_calculated"
)

// CheckoutSettlementInput is the shared input for preview and finalize.
type CheckoutSettlementInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	LeaseID             string
	CheckoutDate        time.Time
	Reason              string
	FinalMeterReading   *int
	CleaningFee         int
	KeyCardLossFee      int
	OtherFee            int
	OtherFeeReason      *string
	Notes               *string
	PreviewToken        string
}

// CheckoutSettlementLine is one typed settlement item.
type CheckoutSettlementLine struct {
	Kind        string                 `json:"kind"`
	Label       string                 `json:"label"`
	Direction   string                 `json:"direction"`
	Amount      int                    `json:"amount"`
	Description *string                `json:"description,omitempty"`
	SourceRef   map[string]interface{} `json:"source_ref,omitempty"`
}

// CheckoutSettlementBlocker prevents finalize.
type CheckoutSettlementBlocker struct {
	Code     string  `json:"code"`
	Message  string  `json:"message"`
	SourceID *string `json:"source_id,omitempty"`
}

// CheckoutSettlementWarning is informational guidance for frontend display.
type CheckoutSettlementWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CheckoutSettlement is the backend-owned checkout settlement snapshot.
type CheckoutSettlement struct {
	LeaseID           string                      `json:"lease_id"`
	PropertyID        string                      `json:"property_id"`
	TenantID          string                      `json:"tenant_id"`
	RoomID            string                      `json:"room_id"`
	PropertyLabel     string                      `json:"property_label"`
	TenantLabel       string                      `json:"tenant_label"`
	RoomLabel         string                      `json:"room_label"`
	CheckoutDate      time.Time                   `json:"checkout_date"`
	Reason            string                      `json:"reason"`
	FinalMeterReading *int                        `json:"final_meter_reading,omitempty"`
	Notes             *string                     `json:"notes,omitempty"`
	Lines             []CheckoutSettlementLine    `json:"lines"`
	Blockers          []CheckoutSettlementBlocker `json:"blockers"`
	Warnings          []CheckoutSettlementWarning `json:"warnings"`
	DepositAmount     int                         `json:"deposit_amount"`
	TotalRefund       int                         `json:"total_refund"`
	TotalCharge       int                         `json:"total_charge"`
	NetAmount         int                         `json:"net_amount"`
	NetDirection      string                      `json:"net_direction"`
	PreviewToken      *string                     `json:"preview_token,omitempty"`
	ExportAvailable   bool                        `json:"export_available"`
	FinalizedAt       *time.Time                  `json:"finalized_at,omitempty"`
}

// PreviewCheckoutSettlementService calculates checkout settlement without mutation.
type PreviewCheckoutSettlementService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewPreviewCheckoutSettlementService returns a PreviewCheckoutSettlementService.
func NewPreviewCheckoutSettlementService(repo Repository, txRunner *txrunner.Runner) *PreviewCheckoutSettlementService {
	return &PreviewCheckoutSettlementService{repo: repo, txRunner: txRunner}
}

// Execute returns the backend-owned settlement proposal.
func (s *PreviewCheckoutSettlementService) Execute(ctx context.Context, input CheckoutSettlementInput) (*CheckoutSettlement, error) {
	leaseID, err := normalizeLeaseID(input.LeaseID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeCheckoutSettlementInput(input)
	if err != nil {
		return nil, err
	}
	normalized.LeaseID = leaseID

	var settlement *CheckoutSettlement
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.repo.FindCheckoutSettlementContextForUpdate(ctx, tx, leaseID)
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if err := authorizeCheckoutSettlement(normalized.ActorRole, normalized.AssignedPropertyIDs, current.Lease.PropertyID); err != nil {
			return err
		}
		bills, err := s.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		settlement = buildCheckoutSettlement(*current, bills, normalized, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return settlement, nil
}

// FinalizeCheckoutSettlementService finalizes checkout settlement atomically.
type FinalizeCheckoutSettlementService struct {
	repo           Repository
	accountingRepo DepositAccountingRepository
	txRunner       *txrunner.Runner
}

// NewFinalizeCheckoutSettlementService returns a FinalizeCheckoutSettlementService.
func NewFinalizeCheckoutSettlementService(repo Repository, accountingRepo DepositAccountingRepository, txRunner *txrunner.Runner) *FinalizeCheckoutSettlementService {
	return &FinalizeCheckoutSettlementService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute verifies the preview token, persists the settlement, and terminates the lease.
func (s *FinalizeCheckoutSettlementService) Execute(ctx context.Context, input CheckoutSettlementInput) (*CheckoutSettlement, error) {
	leaseID, err := normalizeLeaseID(input.LeaseID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeCheckoutSettlementInput(input)
	if err != nil {
		return nil, err
	}
	normalized.LeaseID = leaseID
	normalized.PreviewToken = strings.TrimSpace(input.PreviewToken)
	if normalized.PreviewToken == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "preview_token"})
	}

	var finalized *CheckoutSettlement
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindCheckoutSettlementContextForUpdate(ctx, tx, leaseID)
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if err := authorizeCheckoutSettlement(normalized.ActorRole, normalized.AssignedPropertyIDs, current.Lease.PropertyID); err != nil {
			return err
		}
		bills, err := s.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		proposal := buildCheckoutSettlement(*current, bills, normalized, nil)
		if len(proposal.Blockers) > 0 {
			return errCheckoutSettlementBlocked.WithDetails(map[string]interface{}{"blockers": proposal.Blockers})
		}
		if proposal.PreviewToken == nil || *proposal.PreviewToken != normalized.PreviewToken {
			return errCheckoutSettlementStale
		}

		refundAmount := proposal.DepositAmount - proposal.TotalCharge
		if refundAmount < 0 {
			refundAmount = 0
		}
		deductionAmount := proposal.TotalCharge
		if deductionAmount > proposal.DepositAmount {
			deductionAmount = proposal.DepositAmount
		}
		deductionReason := checkoutDeductionReason(proposal)

		aggregate, err := domainlease.Rehydrate(domainlease.State{
			ID:                        current.Lease.ID,
			TenantID:                  current.Lease.TenantID,
			RoomID:                    current.Lease.RoomID,
			PropertyID:                current.Lease.PropertyID,
			RentAmount:                current.Lease.RentAmount,
			StartDate:                 current.Lease.StartDate,
			EndDate:                   current.Lease.EndDate,
			RentBillingCadence:        current.Lease.RentBillingCadence,
			ElectricityBillingCadence: current.Lease.ElectricityBillingCadence,
			Status:                    current.Lease.Status,
			DepositAmount:             current.Lease.DepositAmount,
			DepositRefundAmount:       current.Lease.DepositRefundAmount,
			DepositDeductionAmount:    current.Lease.DepositDeductionAmount,
			DepositStatus:             current.Lease.DepositStatus,
			DepositDeductionReason:    current.Lease.DepositDeductionReason,
			Version:                   current.Lease.Version,
		})
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.SettleDeposit(refundAmount, deductionAmount, deductionReason); err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.Terminate(); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		settled, err := s.repo.SettleDeposit(ctx, tx, SettleDepositParams{
			LeaseID:                leaseID,
			RefundAmount:           *state.DepositRefundAmount,
			DeductionAmount:        *state.DepositDeductionAmount,
			DepositDeductionReason: state.DepositDeductionReason,
		})
		if err != nil {
			return mapLeaseLookupError(err)
		}

		occurredAt := time.Now().UTC()
		proposal.FinalizedAt = &occurredAt
		proposal.ExportAvailable = true
		proposal.PreviewToken = nil
		detail, err := checkoutSettlementDetailMap(proposal)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		terminatedLease, err := s.repo.TerminateLease(ctx, tx, TerminateLeaseParams{
			LeaseID:           leaseID,
			EndDate:           normalizeDate(normalized.CheckoutDate),
			TerminationReason: normalized.Reason,
			SettlementDetail:  detail,
		})
		if err != nil {
			return mapLeaseLookupError(err)
		}

		if err := recordDepositAccountingEntries(ctx, tx, s.accountingRepo, settled, refundAmount, deductionAmount, depositReasonValue(deductionReason), occurredAt); err != nil {
			return err
		}
		if refundAmount > 0 {
			recorder.Record(domainevents.DepositRefunded{
				LeaseID:    settled.ID,
				PropertyID: settled.PropertyID,
				Amount:     refundAmount,
				OccurredAt: occurredAt,
			})
		}
		if deductionAmount > 0 {
			recorder.Record(domainevents.DepositDeducted{
				LeaseID:    settled.ID,
				PropertyID: settled.PropertyID,
				Amount:     deductionAmount,
				Reason:     depositReasonValue(deductionReason),
				OccurredAt: occurredAt,
			})
		}
		recorder.Record(domainevents.LeaseTerminated{
			LeaseID:       terminatedLease.ID,
			RoomID:        terminatedLease.RoomID,
			PropertyID:    terminatedLease.PropertyID,
			TenantID:      terminatedLease.TenantID,
			Forced:        false,
			IsReplacement: false,
			OccurredAt:    occurredAt,
		})

		finalized = proposal
		return nil
	})
	if err != nil {
		return nil, err
	}

	return finalized, nil
}

// ExportCheckoutSettlementService renders a finalized checkout settlement snapshot.
type ExportCheckoutSettlementService struct {
	repo     Repository
	renderer reporthtml.Renderer
	txRunner *txrunner.Runner
}

// NewExportCheckoutSettlementService returns an ExportCheckoutSettlementService.
func NewExportCheckoutSettlementService(repo Repository, renderer reporthtml.Renderer, txRunner *txrunner.Runner) *ExportCheckoutSettlementService {
	return &ExportCheckoutSettlementService{repo: repo, renderer: renderer, txRunner: txRunner}
}

// Execute renders finalized checkout settlement HTML.
func (s *ExportCheckoutSettlementService) Execute(ctx context.Context, input CheckoutSettlementInput) (*reporthtml.Document, error) {
	leaseID, err := normalizeLeaseID(input.LeaseID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ActorRole) == "" {
		return nil, apperr.ErrUnauthorized
	}

	var settlement *CheckoutSettlement
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.repo.FindCheckoutSettlementContext(ctx, tx, leaseID)
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if err := authorizeCheckoutSettlement(input.ActorRole, input.AssignedPropertyIDs, current.Lease.PropertyID); err != nil {
			return err
		}
		if current.Lease.SettlementDetail == nil {
			return errCheckoutSettlementNotFound
		}
		decoded, err := checkoutSettlementFromDetailMap(*current.Lease.SettlementDetail)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "checkout_settlement_snapshot"})
		}
		if decoded.FinalizedAt == nil {
			return errCheckoutSettlementNotFound
		}
		settlement = decoded
		return nil
	})
	if err != nil {
		return nil, err
	}

	html, err := s.renderer.Render("checkout_settlement.html", newCheckoutSettlementView(settlement))
	if err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "checkout_settlement_renderer"})
	}

	return &reporthtml.Document{
		HTML:     html,
		Filename: reporthtml.HTMLFilename("checkout-settlement", settlement.RoomLabel, settlement.CheckoutDate.Format("2006-01-02")),
	}, nil
}

func normalizeCheckoutSettlementInput(input CheckoutSettlementInput) (CheckoutSettlementInput, error) {
	input.ActorRole = strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch input.ActorRole {
	case "admin", "organizer", "staff":
	default:
		return input, apperr.ErrForbidden
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return input, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "reason"})
	}
	if input.CheckoutDate.IsZero() {
		return input, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "checkout_date"})
	}
	if input.FinalMeterReading != nil && *input.FinalMeterReading < 0 {
		return input, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "final_meter_reading"})
	}
	if input.CleaningFee < 0 || input.KeyCardLossFee < 0 || input.OtherFee < 0 {
		return input, errSettlementNegative
	}
	input.OtherFeeReason = trimmedStringPtr(input.OtherFeeReason)
	input.Notes = trimmedStringPtr(input.Notes)
	return input, nil
}

func authorizeCheckoutSettlement(actorRole string, assignedPropertyIDs []string, propertyID string) error {
	switch strings.ToLower(strings.TrimSpace(actorRole)) {
	case "admin":
		return nil
	case "organizer", "staff":
		if containsAssignedProperty(assignedPropertyIDs, propertyID) {
			return nil
		}
		return apperr.ErrForbidden
	default:
		return apperr.ErrForbidden
	}
}

func buildCheckoutSettlement(current CheckoutSettlementContext, bills []Bill, input CheckoutSettlementInput, finalizedAt *time.Time) *CheckoutSettlement {
	lines := []CheckoutSettlementLine{
		{
			Kind:      checkoutLineDepositRefund,
			Label:     "押金退還",
			Direction: checkoutDirectionRefund,
			Amount:    current.Lease.DepositAmount,
		},
	}
	if input.CleaningFee > 0 {
		lines = append(lines, CheckoutSettlementLine{Kind: checkoutLineCleaningFee, Label: "清潔費", Direction: checkoutDirectionCharge, Amount: input.CleaningFee})
	}
	if input.KeyCardLossFee > 0 {
		lines = append(lines, CheckoutSettlementLine{Kind: checkoutLineKeyCardLoss, Label: "門禁卡/鑰匙遺失費", Direction: checkoutDirectionCharge, Amount: input.KeyCardLossFee})
	}
	if input.OtherFee > 0 {
		lines = append(lines, CheckoutSettlementLine{Kind: checkoutLineOtherFee, Label: "其他費用", Direction: checkoutDirectionCharge, Amount: input.OtherFee, Description: input.OtherFeeReason})
	}
	if input.FinalMeterReading != nil {
		lines = append(lines, CheckoutSettlementLine{
			Kind:        checkoutLineElectricSettlement,
			Label:       "退租電表讀數",
			Direction:   checkoutDirectionInfo,
			Amount:      0,
			Description: stringPtr("電費結算由後端保留為快照資訊；v1 不由 frontend 計算。"),
			SourceRef:   map[string]interface{}{"final_meter_reading": *input.FinalMeterReading},
		})
	}

	blockers := checkoutSettlementBlockers(current.Lease, bills, input, lines)
	warnings := checkoutSettlementWarnings(current.Lease, input)
	totalRefund, totalCharge := checkoutSettlementTotals(lines)
	netDirection, netAmount := checkoutSettlementNet(totalRefund, totalCharge)

	settlement := &CheckoutSettlement{
		LeaseID:           current.Lease.ID,
		PropertyID:        current.Lease.PropertyID,
		TenantID:          current.Lease.TenantID,
		RoomID:            current.Lease.RoomID,
		PropertyLabel:     current.PropertyName,
		TenantLabel:       current.TenantName,
		RoomLabel:         current.RoomName,
		CheckoutDate:      normalizeDate(input.CheckoutDate),
		Reason:            input.Reason,
		FinalMeterReading: input.FinalMeterReading,
		Notes:             input.Notes,
		Lines:             lines,
		Blockers:          blockers,
		Warnings:          warnings,
		DepositAmount:     current.Lease.DepositAmount,
		TotalRefund:       totalRefund,
		TotalCharge:       totalCharge,
		NetAmount:         netAmount,
		NetDirection:      netDirection,
		ExportAvailable:   finalizedAt != nil,
		FinalizedAt:       finalizedAt,
	}
	if len(blockers) == 0 && finalizedAt == nil {
		token := checkoutSettlementToken(current.Lease, bills, input, lines, totalRefund, totalCharge)
		settlement.PreviewToken = &token
	}
	return settlement
}

func checkoutSettlementBlockers(lease Lease, bills []Bill, input CheckoutSettlementInput, lines []CheckoutSettlementLine) []CheckoutSettlementBlocker {
	blockers := make([]CheckoutSettlementBlocker, 0)
	if lease.Status != domainlease.StatusActive && lease.Status != domainlease.StatusExpired {
		blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerLeaseNotActive, Message: "租約狀態不可執行正常退租結算。"})
	}
	if lease.DepositStatus != domainlease.DepositStatusHeld {
		blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerDepositNotHeld, Message: "押金狀態不是 held，無法執行退租結算。"})
	}
	if normalizeDate(input.CheckoutDate).Before(normalizeDate(lease.EndDate)) {
		blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerRentRefund, Message: "v1 尚未自動計算未到期租金退款，不能定稿退租結算。"})
	}
	for _, bill := range bills {
		switch bill.Status {
		case "pending_payment", "overdue":
			sourceID := bill.ID
			blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerUnpaidBill, Message: "租約仍有未結清帳單。", SourceID: &sourceID})
		case "pending_meter":
			sourceID := bill.ID
			blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerPendingMeter, Message: "租約仍有待抄表帳單。", SourceID: &sourceID})
		}
	}
	_, totalCharge := checkoutSettlementTotals(lines)
	if totalCharge > lease.DepositAmount {
		blockers = append(blockers, CheckoutSettlementBlocker{Code: checkoutBlockerChargeExceedsDep, Message: "v1 退租扣款不可超過押金金額。"})
	}
	return blockers
}

func checkoutSettlementWarnings(lease Lease, input CheckoutSettlementInput) []CheckoutSettlementWarning {
	warnings := make([]CheckoutSettlementWarning, 0)
	if input.FinalMeterReading != nil {
		warnings = append(warnings, CheckoutSettlementWarning{Code: checkoutWarningFinalMeter, Message: "退租電表讀數已保存於結算快照；本次不由前端計算電費。"})
	}
	return warnings
}

func checkoutSettlementTotals(lines []CheckoutSettlementLine) (int, int) {
	totalRefund := 0
	totalCharge := 0
	for _, line := range lines {
		switch line.Direction {
		case checkoutDirectionRefund:
			totalRefund += line.Amount
		case checkoutDirectionCharge:
			totalCharge += line.Amount
		}
	}
	return totalRefund, totalCharge
}

func checkoutSettlementNet(totalRefund int, totalCharge int) (string, int) {
	switch {
	case totalRefund > totalCharge:
		return checkoutNetRefund, totalRefund - totalCharge
	case totalCharge > totalRefund:
		return checkoutNetPayable, totalCharge - totalRefund
	default:
		return checkoutNetZero, 0
	}
}

func checkoutSettlementToken(lease Lease, bills []Bill, input CheckoutSettlementInput, lines []CheckoutSettlementLine, totalRefund int, totalCharge int) string {
	payload := map[string]interface{}{
		"lease_id":       lease.ID,
		"lease_version":  lease.Version,
		"checkout_date":  normalizeDate(input.CheckoutDate).Format("2006-01-02"),
		"reason":         input.Reason,
		"final_meter":    input.FinalMeterReading,
		"cleaning_fee":   input.CleaningFee,
		"key_card_fee":   input.KeyCardLossFee,
		"other_fee":      input.OtherFee,
		"other_reason":   stringValue(input.OtherFeeReason),
		"notes":          stringValue(input.Notes),
		"bills":          bills,
		"lines":          lines,
		"total_refund":   totalRefund,
		"total_charge":   totalCharge,
		"deposit_amount": lease.DepositAmount,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func checkoutDeductionReason(settlement *CheckoutSettlement) *string {
	reasons := make([]string, 0)
	for _, line := range settlement.Lines {
		if line.Direction != checkoutDirectionCharge || line.Amount <= 0 {
			continue
		}
		if line.Description != nil && strings.TrimSpace(*line.Description) != "" {
			reasons = append(reasons, line.Label+": "+strings.TrimSpace(*line.Description))
			continue
		}
		reasons = append(reasons, line.Label)
	}
	if len(reasons) == 0 {
		return nil
	}
	value := strings.Join(reasons, "、")
	return &value
}

func checkoutSettlementDetailMap(settlement *CheckoutSettlement) (map[string]interface{}, error) {
	data, err := json.Marshal(settlement)
	if err != nil {
		return nil, err
	}
	var detail map[string]interface{}
	if err := json.Unmarshal(data, &detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func checkoutSettlementFromDetailMap(detail map[string]interface{}) (*CheckoutSettlement, error) {
	data, err := json.Marshal(detail)
	if err != nil {
		return nil, err
	}
	var settlement CheckoutSettlement
	if err := json.Unmarshal(data, &settlement); err != nil {
		return nil, err
	}
	return &settlement, nil
}

func stringPtr(value string) *string {
	return &value
}

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
