package readstate

import "fmt"

// PlanMigration is ReadStateMigrationPlanner.operations. Row order comes from
// a revision-fenced export; per-row ordering preserves explicit unread
// precedence and original read times. No source data is deleted here.
func PlanMigration(rows []LegacyRow, migrationID string) ([]Operation, error) {
	if migrationID == "" || len(migrationID) > 100 {
		return nil, ErrInvalidRecord
	}
	previousPhase := 0
	operations := make([]Operation, 0, len(rows))
	for index, row := range rows {
		var phase int
		switch row.Kind {
		case LegacyBoundary:
			phase = 0
		case LegacyUnread:
			phase = 1
		case LegacyRead:
			phase = 2
		default:
			return nil, ErrInvalidRecord
		}
		if phase < previousPhase {
			return nil, ErrInvalidRecord
		}
		previousPhase = phase
		actionID := fmt.Sprintf("%s-%d", migrationID, index)
		var operation Operation
		if row.Kind == LegacyBoundary {
			if row.Boundary == nil || row.SubjectURI != nil {
				return nil, ErrInvalidRecord
			}
			operation = NewBoundaryOperation(actionID, int64(index+1), StateRead, row.ActedAt, []Boundary{*row.Boundary})
		} else {
			if row.SubjectURI == nil || row.Boundary != nil {
				return nil, ErrInvalidRecord
			}
			state := StateUnread
			if row.Kind == LegacyRead {
				state = StateRead
			}
			operation = NewExactOperation(actionID, int64(index+1), state, row.ActedAt, []string{*row.SubjectURI}, nil)
		}
		if err := ValidateOperation(operation); err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}
