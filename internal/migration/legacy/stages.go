package legacy

import (
	"fmt"
	"strings"
)

// Stage identifies a legacy migration execution slice.
type Stage string

const (
	StageProperties Stage = "properties"
	StageRooms      Stage = "rooms"
	StageTenants    Stage = "tenants"
	StageLeases     Stage = "leases"
	StageRoomStatus Stage = "room-status"
	StageBills      Stage = "bills"
	StageJournal    Stage = "journal"
)

var validStages = map[Stage]struct{}{
	StageProperties: {},
	StageRooms:      {},
	StageTenants:    {},
	StageLeases:     {},
	StageRoomStatus: {},
	StageBills:      {},
	StageJournal:    {},
}

// DefaultStages returns the execution order that downstream migration tasks depend on.
func DefaultStages() []Stage {
	return []Stage{
		StageProperties,
		StageRooms,
		StageTenants,
		StageLeases,
		StageRoomStatus,
		StageBills,
		StageJournal,
	}
}

// ParseStage converts CLI input into a supported stage identifier.
func ParseStage(value string) (Stage, error) {
	stage := Stage(strings.TrimSpace(value))
	if _, ok := validStages[stage]; !ok {
		return "", fmt.Errorf("unsupported stage %q", value)
	}

	return stage, nil
}
