package journal

import "errors"

var (
	ErrContentRequired = errors.New("journal content required")
	ErrNilAggregate    = errors.New("journal aggregate is nil")
)
