package legacy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type sourceSpec struct {
	FileName    string
	RootKey     string
	LegacyTable string
	RequiredBy  []Stage
}

var sourceSpecs = []sourceSpec{
	{FileName: "01_estate.json", RootKey: "xx_estate", LegacyTable: "xx_estate", RequiredBy: []Stage{StageProperties}},
	{FileName: "01_estate_room.json", RootKey: "xx_estate_room", LegacyTable: "xx_estate_room", RequiredBy: []Stage{StageRooms}},
	{FileName: "02_estate_user.json", RootKey: "xx_estate_user", LegacyTable: "xx_estate_user", RequiredBy: []Stage{StageTenants}},
	{FileName: "02_estate_rent.json", RootKey: "xx_estate_rent", LegacyTable: "xx_estate_rent", RequiredBy: []Stage{StageLeases}},
	{FileName: "02_estate_rent_user.json", RootKey: "xx_estate_rent_user", LegacyTable: "xx_estate_rent_user", RequiredBy: []Stage{StageLeases}},
	{FileName: "02_estate_electric.json", RootKey: "xx_estate_electric", LegacyTable: "xx_estate_electric", RequiredBy: []Stage{StageBills}},
	{FileName: "02_estate_schedule.json", RootKey: "xx_estate_schedule", LegacyTable: "xx_estate_schedule", RequiredBy: []Stage{StageJournal}},
	{FileName: "02_estate_reply.json", RootKey: "xx_estate_reply", LegacyTable: "xx_estate_reply", RequiredBy: []Stage{StageJournal}},
}

func inspectSources(sourceDir string, stages []Stage) ([]SourceFileReport, error) {
	selected := selectedStages(stages)
	reports := make([]SourceFileReport, 0, len(sourceSpecs))

	for _, spec := range sourceSpecs {
		requiredBy := intersectStages(spec.RequiredBy, selected)
		if len(requiredBy) == 0 {
			continue
		}

		path := filepath.Join(sourceDir, spec.FileName)
		recordCount, err := inspectSourceFile(path, spec.RootKey)
		if err != nil {
			return nil, err
		}

		reports = append(reports, SourceFileReport{
			Path:        path,
			RootKey:     spec.RootKey,
			RecordCount: recordCount,
			RequiredBy:  requiredBy,
			LegacyTable: spec.LegacyTable,
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Path < reports[j].Path
	})

	return reports, nil
}

func inspectSourceFile(path string, rootKey string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return 0, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[rootKey]
	if !ok {
		return 0, fmt.Errorf("source file %s missing root key %q", path, rootKey)
	}

	var records []json.RawMessage
	if err := json.Unmarshal(rawRecords, &records); err != nil {
		return 0, fmt.Errorf("decode records for %s in %s: %w", rootKey, path, err)
	}

	return len(records), nil
}

func selectedStages(stages []Stage) map[Stage]struct{} {
	result := make(map[Stage]struct{}, len(stages))
	for _, stage := range stages {
		result[stage] = struct{}{}
	}

	return result
}

func intersectStages(candidates []Stage, selected map[Stage]struct{}) []Stage {
	result := make([]Stage, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := selected[candidate]; ok {
			result = append(result, candidate)
		}
	}

	return result
}
