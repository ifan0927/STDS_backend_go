package repair

import "errors"

var (
	ErrTitleRequired            = errors.New("repair title required")
	ErrInvalidStatus            = errors.New("repair invalid status")
	ErrInvalidStatusForAssign   = errors.New("repair invalid status for assign")
	ErrInvalidStatusForProgress = errors.New("repair invalid status for progress")
	ErrInvalidStatusForComplete = errors.New("repair invalid status for complete")
	ErrRepairAlreadyCompleted   = errors.New("repair already completed")
	ErrInvalidStatusForCancel   = errors.New("repair invalid status for cancel")
)
