package billing

import (
	"fmt"

	domainbilling "stds_backend/internal/domain/billing"
)

const (
	AccountingCategoryRentPayment         = "rent_payment"
	AccountingCategoryElectricityPayment  = "electricity_payment"
	AccountingTitleCodeRentPayment        = "4603"
	AccountingTitleCodeElectricityPayment = "4605"
)

func accountingCategoryForBillType(billType string) (string, error) {
	switch billType {
	case domainbilling.TypeRent:
		return AccountingCategoryRentPayment, nil
	case domainbilling.TypeElectricity:
		return AccountingCategoryElectricityPayment, nil
	default:
		return "", fmt.Errorf("unsupported bill type for accounting entry: %s", billType)
	}
}

func accountingTitleCodeForBillType(billType string) (string, error) {
	switch billType {
	case domainbilling.TypeRent:
		return AccountingTitleCodeRentPayment, nil
	case domainbilling.TypeElectricity:
		return AccountingTitleCodeElectricityPayment, nil
	default:
		return "", fmt.Errorf("unsupported bill type for accounting title: %s", billType)
	}
}
