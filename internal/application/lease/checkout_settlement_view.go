package lease

import (
	"strconv"
	"time"
)

type checkoutSettlementView struct {
	PropertyLabel          string
	TenantLabel            string
	RoomLabel              string
	CheckoutDateLabel      string
	ActualMoveOutDateLabel string
	Reason                 string
	FinalMeterReading      *int
	ManualRentRefundReason *string
	Notes                  *string
	Lines                  []checkoutSettlementLineView
	TotalRefundLabel       string
	TotalChargeLabel       string
	NetLabel               string
	NetDirectionLabel      string
	FinalizedAtLabel       string
}

type checkoutSettlementLineView struct {
	Label       string
	Direction   string
	AmountLabel string
	Description *string
}

func newCheckoutSettlementView(settlement *CheckoutSettlement) checkoutSettlementView {
	lines := make([]checkoutSettlementLineView, 0, len(settlement.Lines))
	for _, line := range settlement.Lines {
		lines = append(lines, checkoutSettlementLineView{
			Label:       line.Label,
			Direction:   checkoutDirectionLabel(line.Direction),
			AmountLabel: formatCheckoutAmount(line.Amount),
			Description: line.Description,
		})
	}

	finalizedAt := ""
	if settlement.FinalizedAt != nil {
		finalizedAt = settlement.FinalizedAt.In(time.FixedZone("Asia/Taipei", 8*60*60)).Format("2006-01-02 15:04")
	}

	return checkoutSettlementView{
		PropertyLabel:          settlement.PropertyLabel,
		TenantLabel:            settlement.TenantLabel,
		RoomLabel:              settlement.RoomLabel,
		CheckoutDateLabel:      settlement.CheckoutDate.Format("2006-01-02"),
		ActualMoveOutDateLabel: checkoutDatePtrLabel(settlement.ActualMoveOutDate),
		Reason:                 settlement.Reason,
		FinalMeterReading:      settlement.FinalMeterReading,
		ManualRentRefundReason: settlement.ManualRentRefundReason,
		Notes:                  settlement.Notes,
		Lines:                  lines,
		TotalRefundLabel:       formatCheckoutAmount(settlement.TotalRefund),
		TotalChargeLabel:       formatCheckoutAmount(settlement.TotalCharge),
		NetLabel:               formatCheckoutAmount(settlement.NetAmount),
		NetDirectionLabel:      checkoutNetDirectionLabel(settlement.NetDirection),
		FinalizedAtLabel:       finalizedAt,
	}
}

func checkoutDatePtrLabel(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

func checkoutDirectionLabel(direction string) string {
	switch direction {
	case checkoutDirectionRefund:
		return "退還"
	case checkoutDirectionCharge:
		return "扣款"
	default:
		return "資訊"
	}
}

func checkoutNetDirectionLabel(direction string) string {
	switch direction {
	case checkoutNetRefund:
		return "應退還"
	case checkoutNetPayable:
		return "應補繳"
	default:
		return "結清"
	}
}

func formatCheckoutAmount(amount int) string {
	return "NT$" + strconv.Itoa(amount)
}
