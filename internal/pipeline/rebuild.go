package pipeline

import (
	"fmt"

	"zhuimi/internal/config"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func RebuildReports(cfg config.Config, db *store.Store, date string, compilePDF bool) error {
	dates := db.ListReportDates()
	if date != "" {
		dates = []string{date}
	}
	if len(dates) == 0 {
		fmt.Println(`{"rebuild":0,"message":"no reports found"}`)
		return nil
	}

	rebuilt := 0
	for _, reportDate := range dates {
		stored := db.Report(reportDate)
		if stored == nil {
			if date != "" {
				return fmt.Errorf("report not found: %s", reportDate)
			}
			continue
		}
		articles := db.ArticlesByIDs(stored.ArticleIDs)
		if len(articles) == 0 {
			if date != "" {
				return fmt.Errorf("report has no article snapshots: %s", reportDate)
			}
			continue
		}
		if err := report.WriteDailyWithOptions(cfg, reportDate, articles, report.WriteOptions{Mode: stored.Mode}); err != nil {
			return fmt.Errorf("rebuild report %s: %w", reportDate, err)
		}
		if compilePDF {
			if err := report.CompileDailyPDF(cfg, reportDate); err != nil {
				return fmt.Errorf("compile report pdf %s: %w", reportDate, err)
			}
		}
		rebuilt++
	}

	if err := report.WriteIndex(cfg, db.ListReportDates()); err != nil {
		return fmt.Errorf("rebuild index: %w", err)
	}
	fmt.Printf(`{"rebuild":%d,"date":"%s","pdf":%t}`+"\n", rebuilt, date, compilePDF)
	return nil
}
