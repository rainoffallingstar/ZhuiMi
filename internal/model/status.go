package model

import "strings"

func NormalizeContentStatus(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", ContentStatusPending, ContentStatusFetched, ContentStatusRSSFallback, ContentStatusFailed, ContentStatusSkipped, "missing":
		return value
	default:
		return value
	}
}

func NormalizeProcessorStatus(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", ProcessorStatusProcessed, ProcessorStatusFailed, ProcessorStatusPending, ProcessorStatusSkipped, "missing":
		return value
	default:
		return value
	}
}
