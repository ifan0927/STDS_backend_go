package lease

import (
	"strconv"
	"strings"
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
			Description: checkoutSettlementLineDescription(line),
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

func checkoutSettlementLineDescription(line CheckoutSettlementLine) *string {
	description := line.Description
	if line.Kind != checkoutLineElectricSettlement {
		return description
	}

	detail, ok := checkoutElectricitySettlementDetail(line.SourceRef)
	if !ok {
		return description
	}
	if description == nil || strings.TrimSpace(*description) == "" {
		return &detail
	}

	combined := strings.TrimSpace(*description) + "\n" + detail
	return &combined
}

func checkoutElectricitySettlementDetail(sourceRef map[string]interface{}) (string, bool) {
	previousReading, ok := checkoutSourceRefDisplayValue(sourceRef["previous_reading"])
	if !ok {
		return "", false
	}
	currentReading, ok := checkoutSourceRefDisplayValue(sourceRef["final_meter_reading"])
	if !ok {
		currentReading, ok = checkoutSourceRefDisplayValue(sourceRef["current_reading"])
	}
	if !ok {
		return "", false
	}
	usage, ok := checkoutSourceRefDisplayValue(sourceRef["usage"])
	if !ok {
		return "", false
	}
	unitPrice, ok := checkoutSourceRefDisplayValue(sourceRef["unit_price"])
	if !ok {
		return "", false
	}

	detail := "前次讀數 " + previousReading + "，退租讀數 " + currentReading + "，用電 " + usage + " 度，單價 NT$" + unitPrice + "/度"
	return detail, true
}

func checkoutSourceRefDisplayValue(value interface{}) (string, bool) {
	switch v := value.(type) {
	case int:
		return strconv.Itoa(v), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case string:
		trimmed := strings.TrimSpace(v)
		return trimmed, trimmed != ""
	default:
		return "", false
	}
}
