package lease

import (
	"strings"
	"testing"
)

func TestCheckoutSettlementTemplateUsesA4PageLayout(t *testing.T) {
	manualRentRefundReason := "承租人確認不退租金；這是一段很長的人工租金退回決策說明，需要留在 A4 預覽頁面內換行。"
	description := "這是一段很長而且包含不安全內容 <script>alert('description')</script> 的明細說明，需要限制在欄位內。"
	notes := "第一行備註\n第二行是很長的備註內容，需要留在頁面內換行，不能把 A4 預覽頁面撐寬。"
	view := checkoutSettlementView{
		PropertyLabel:          "很長很長的測試物業名稱",
		TenantLabel:            "測試租客",
		RoomLabel:              "A-1001-很長的房號",
		CheckoutDateLabel:      "2026-12-31",
		ActualMoveOutDateLabel: "2026-12-30",
		Reason:                 "租客申請退租",
		ManualRentRefundReason: &manualRentRefundReason,
		Notes:                  &notes,
		Lines: []checkoutSettlementLineView{{
			Label:       "其他費用",
			Direction:   "扣款",
			AmountLabel: "NT$500",
			Description: &description,
		}},
		TotalRefundLabel:  "NT$20000",
		TotalChargeLabel:  "NT$500",
		NetLabel:          "NT$19500",
		NetDirectionLabel: "應退還",
		FinalizedAtLabel:  "2026-12-31 18:00",
	}

	html, err := MustNewCheckoutSettlementRenderer().Render("checkout_settlement.html", view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rendered := string(html)
	for _, want := range []string{
		`<main class="page">`,
		`width: 210mm;`,
		`min-height: 297mm;`,
		`table-layout: fixed;`,
		`overflow-wrap: anywhere;`,
		`size: A4 portrait;`,
		manualRentRefundReason,
		notes,
		`&lt;script&gt;alert(&#39;description&#39;)&lt;/script&gt;`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered checkout settlement HTML missing %q", want)
		}
	}
	if strings.Contains(rendered, "<script>") {
		t.Fatal("rendered checkout settlement HTML contains an unescaped script element")
	}
}
