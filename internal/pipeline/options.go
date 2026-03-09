package pipeline

import "zhuimi/internal/model"

type RunOptions struct {
	SkipScoring bool
	CompilePDF  bool
}

func (o RunOptions) ReportMode() string {
	if o.SkipScoring {
		return model.ReportModeRaw
	}
	return model.ReportModeScored
}
