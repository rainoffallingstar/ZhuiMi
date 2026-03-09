package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLegacyReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.typ")
	content := `#import "../../../config.typ": template, tufted
#show: template.with(title: "追觅 - 2026-03-08")
+
== #1. Demo Title
+
- **研究分数**: #91
- **社会影响**: #77
- **血液相关性**: #88
- **推荐度**: #90
- **DOI**: #10.1000/demo
- **链接**: #link("https://pubmed.ncbi.nlm.nih.gov/123/")[查看原文]
+
推荐理由: 值得阅读
+
摘要: Demo abstract
+
---
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := parseLegacyReport(path, "2026-03-08", "legacy://report-import")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 article, got %d", len(items))
	}
	if items[0].article.ID != "doi:10.1000/demo" {
		t.Fatalf("unexpected article id: %s", items[0].article.ID)
	}
	if items[0].score.Recommendation != 90 {
		t.Fatalf("unexpected recommendation: %d", items[0].score.Recommendation)
	}
}
