package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"stds_backend/internal/config"
	"stds_backend/internal/devseed"
	"stds_backend/internal/platform/database"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: go run ./cmd/dev_seed [%s]", devseed.DemoCommand)
	}
	if os.Args[1] != devseed.DemoCommand {
		log.Fatalf("unsupported seed %q; supported seed: %s", os.Args[1], devseed.DemoCommand)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.DB.URL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	result, err := devseed.Run(ctx, db, devseed.Options{
		AppEnv:      cfg.App.Env,
		DatabaseURL: cfg.DB.URL,
	})
	if err != nil {
		log.Fatalf("run dev seed: %v", err)
	}

	fmt.Printf("dev seed ready: command=%s property_id=%s report_period=%04d-%02d report_mode=%s admin_email=%s paid_rent_bill_id=%s paid_electric_bill_id=%s checkout_lease_id=%s\n",
		result.Command,
		result.PropertyID,
		result.ReportYear,
		result.ReportMonth,
		result.ReportMode,
		result.SeededLocalAdminEmail,
		result.PaidRentBillID,
		result.PaidElectricBillID,
		result.CheckoutLeaseID,
	)
}
