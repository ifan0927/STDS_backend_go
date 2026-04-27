package journal

import (
	"errors"
	"testing"
)

func TestAggregateUpdateRejectsNilReceiver(t *testing.T) {
	var aggregate *Aggregate

	err := aggregate.Update(UpdateInput{})
	if !errors.Is(err, ErrNilAggregate) {
		t.Fatalf("Update() error = %v, want ErrNilAggregate", err)
	}
}
