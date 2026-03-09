package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func PruneReports(cfg config.Config, db *store.Store, keepDays int) error {
	if keepDays < 1 {
		keepDays = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -keepDays)
	cutoffDate := cutoff.Format("2006-01-02")

	reportDates := db.ListReportDates()
	pruned := 0
	for _, reportDate := range reportDates {
		if reportDate >= cutoffDate {
			continue
		}
		if err := db.DeleteReport(reportDate); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(cfg.ContentDir, reportDate, "index.typ"))
		_ = os.Remove(filepath.Join(cfg.ContentDir, reportDate))
		pruned++
	}
	if err := report.WriteIndex(cfg, db.ListReportDates()); err != nil {
		return fmt.Errorf("rewrite index after prune: %w", err)
	}
	fmt.Printf(`{"pruned":%d,"keep_days":%d}`+"\n", pruned, keepDays)
	return nil
}
