package checks

// ErrorsReport captures recent error tail.
// Hermem does not currently persist slog ERROR entries to a queryable
// store. This check returns an empty report with a note explaining
// the limitation.
type ErrorsReport struct {
	Entries []string
	Note    string
}

// CheckErrorsTail returns an error tail report.
// Currently returns an empty list with a note — hermem does not
// persist slog ERROR entries to an indexed table. A future commit
// may add an error_log table or wire structured slog to a file for
// tailing.
func CheckErrorsTail() ErrorsReport {
	return ErrorsReport{
		Entries: nil,
		Note:    "slog ERROR entries are not persisted to a queryable store. Pipe stderr to a file and grep for \"level=ERROR\" to inspect recent errors.",
	}
}
