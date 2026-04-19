package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	legacyScheduleSourceFileName  = "02_estate_schedule.json"
	legacyScheduleSourceRootKey   = "xx_estate_schedule"
	legacyReplySourceFileName     = "02_estate_reply.json"
	legacyReplySourceRootKey      = "xx_estate_reply"
	task12ReportFileName          = "task12_journal_report.json"
	legacyJournalPlaceholderUID   = "legacy-journal-system"
	legacyJournalPlaceholderEmail = "legacy-journal-system@migration.local"
	legacyJournalPlaceholderName  = "Legacy Journal Migration"
	repairRequestStatusSubmitted  = "submitted"
	repairRequestStatusCompleted  = "completed"
	repairRequestStatusCancelled  = "cancelled"
	journalTargetTableName        = "journal_logs"
	repairRequestTargetTableName  = "repair_requests"
	legacyScheduleTimestampLayout = "2006-01-02 15:04:05"
	legacyReplyTimestampLayout    = "2006-01-02 15:04:05"
)

var repairSignalKeywords = []string{
	"維修",
	"修繕",
	"查修",
	"檢修",
	"報修",
	"故障",
	"壞掉",
	"損壞",
	"漏水",
	"不冷",
	"不亮",
	"斷網",
	"更換",
	"門把",
	"門鎖",
	"水龍頭",
	"馬桶",
	"蓮蓬頭",
	"洗衣機",
	"飲水機",
	"熱水器",
	"冷氣",
	"燈管",
}

var repairKinds = map[string]struct{}{
	"叫修": {},
	"維修": {},
	"檢修": {},
}

// MigrateJournalOptions controls Task 12 execution.
type MigrateJournalOptions struct {
	SourceDir string
	ReportDir string
}

// JournalMigrationReport captures Task 12 execution details.
type JournalMigrationReport struct {
	GeneratedAt              time.Time           `json:"generated_at"`
	SourcePath               string              `json:"source_path"`
	ReplySourcePath          string              `json:"reply_source_path"`
	ReportPath               string              `json:"report_path"`
	TotalScheduleRows        int                 `json:"total_schedule_rows"`
	TotalReplyRows           int                 `json:"total_reply_rows"`
	EligibleRows             int                 `json:"eligible_rows"`
	ImportedJournalLogs      int                 `json:"imported_journal_logs"`
	ImportedRepairRequests   int                 `json:"imported_repair_requests"`
	AlreadyMappedRows        int                 `json:"already_mapped_rows"`
	MappingsCreated          int                 `json:"mappings_created"`
	SkippedRows              int                 `json:"skipped_rows"`
	InvalidRows              int                 `json:"invalid_rows"`
	MissingPropertyMappings  int                 `json:"missing_property_mappings"`
	MissingRoomMappings      int                 `json:"missing_room_mappings"`
	NonRoomRepairJournalRows int                 `json:"non_room_repair_journal_rows"`
	RepliesMerged            int                 `json:"replies_merged"`
	OrphanReplyRows          int                 `json:"orphan_reply_rows"`
	PlaceholderAuthorUses    int                 `json:"placeholder_author_uses"`
	PlaceholderAuthorCreated int                 `json:"placeholder_author_created"`
	PlaceholderAuthorReused  int                 `json:"placeholder_author_reused"`
	ClassificationCounts     map[string]int      `json:"classification_counts"`
	RepairStatusCounts       map[string]int      `json:"repair_status_counts"`
	Skipped                  []JournalSkipRecord `json:"skipped"`
	Assumptions              []string            `json:"assumptions"`
}

// JournalSkipRecord captures a skipped schedule row and the reason.
type JournalSkipRecord struct {
	LegacyScheduleID string `json:"legacy_schedule_id"`
	LegacyEstateID   string `json:"legacy_estate_id,omitempty"`
	LegacyRoomID     string `json:"legacy_room_id,omitempty"`
	Reason           string `json:"reason"`
}

type legacyScheduleSource struct {
	Schedules []legacyScheduleRecord `json:"xx_estate_schedule"`
}

type legacyScheduleRecord struct {
	ScheduleID string `json:"estate_schedule_id"`
	RoomID     string `json:"estate_room_id"`
	Date       string `json:"estate_schedule_date"`
	Kind       string `json:"estate_schedule_kind"`
	LegacyUID  string `json:"estate_schedule_uid"`
	Facility   string `json:"estate_schedule_facility"`
	Content    string `json:"estate_schedule_content"`
	AssistUID  string `json:"estate_schedule_assist"`
	Status     string `json:"estate_schedule_status"`
	EstateID   string `json:"estate_id"`
}

type legacyReplySource struct {
	Replies []legacyReplyRecord `json:"xx_estate_reply"`
}

type legacyReplyRecord struct {
	ReplyID    string `json:"estate_reply_id"`
	ScheduleID string `json:"estate_schedule_id"`
	Date       string `json:"estate_reply_date"`
	Content    string `json:"estate_reply_content"`
	LegacyUID  string `json:"estate_reply_uid"`
}

type normalizedJournalSchedule struct {
	LegacyScheduleID string
	LegacyEstateID   string
	LegacyRoomID     string
	PropertyID       string
	RoomID           *string
	CreatedAt        time.Time
	Kind             string
	Status           string
	Facility         string
	Content          string
	LegacyUID        string
	AssistUID        string
	Replies          []normalizedLegacyReply
	TargetTable      string
}

type normalizedLegacyReply struct {
	LegacyReplyID string
	LegacyUID     string
	CreatedAt     time.Time
	Content       string
}

// MigrateJournal executes Task 12 against the configured database.
func MigrateJournal(ctx context.Context, db *sql.DB, options MigrateJournalOptions) (*JournalMigrationReport, error) {
	if options.SourceDir == "" {
		return nil, fmt.Errorf("source dir is required")
	}
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	schedulePath := filepath.Join(options.SourceDir, legacyScheduleSourceFileName)
	replyPath := filepath.Join(options.SourceDir, legacyReplySourceFileName)

	schedules, err := loadLegacySchedules(schedulePath)
	if err != nil {
		return nil, err
	}
	replies, err := loadLegacyReplies(replyPath)
	if err != nil {
		return nil, err
	}

	replyIndex, orphanReplyRows := buildLegacyReplyIndex(schedules, replies)

	report := &JournalMigrationReport{
		GeneratedAt:          time.Now().UTC(),
		SourcePath:           schedulePath,
		ReplySourcePath:      replyPath,
		TotalScheduleRows:    len(schedules),
		TotalReplyRows:       len(replies),
		OrphanReplyRows:      orphanReplyRows,
		ClassificationCounts: make(map[string]int),
		RepairStatusCounts:   make(map[string]int),
		Skipped:              make([]JournalSkipRecord, 0),
		Assumptions: []string{
			"Task 5 mapping-table strategy remains authoritative: property_id and optional room_id are resolved only through legacy_property_mappings and legacy_room_mappings.",
			"Task 12 uses a deterministic placeholder staff user keyed by firebase_uid=legacy-journal-system for journal_logs.author_id and repair_requests.submitted_by because legacy schedule/reply users are not mapped into the current users table.",
			"Original legacy schedule_uid, assist uid, and reply uid values are preserved inside migrated content so Task 12 keeps traceability without inventing unsupported user mappings.",
			"Task 12 routes rows into repair_requests only when the schedule is room-specific and deterministically classified as repair-related by kind or repair-language signals; non-room repair notes stay in journal_logs.",
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin journal migration tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureLegacyScheduleMappingsTable(ctx, tx); err != nil {
		return nil, err
	}

	placeholderAuthorID, created, err := ensureJournalPlaceholderAuthor(ctx, tx)
	if err != nil {
		return nil, err
	}
	if created {
		report.PlaceholderAuthorCreated++
	} else {
		report.PlaceholderAuthorReused++
	}

	for _, schedule := range schedules {
		legacyScheduleID := strings.TrimSpace(schedule.ScheduleID)

		exists, err := journalScheduleMappingExists(ctx, tx, legacyScheduleID)
		if err != nil {
			return nil, err
		}
		if exists {
			report.AlreadyMappedRows++
			continue
		}

		propertyID, propertyFound, err := findPropertyIDByLegacyEstateID(ctx, tx, strings.TrimSpace(schedule.EstateID))
		if err != nil {
			return nil, err
		}

		var roomID *string
		legacyRoomID := strings.TrimSpace(schedule.RoomID)
		roomFound := false
		if legacyRoomID != "" && legacyRoomID != "0" {
			mappedRoomID, found, err := findRoomIDByLegacyRoomID(ctx, tx, legacyRoomID)
			if err != nil {
				return nil, err
			}
			roomFound = found
			if found {
				roomID = &mappedRoomID
			}
		}

		normalized, skipReason, err := normalizeJournalSchedule(schedule, propertyID, propertyFound, roomID, roomFound, replyIndex[legacyScheduleID])
		if err != nil {
			return nil, err
		}
		if skipReason != "" {
			report.SkippedRows++
			report.InvalidRows++
			if !propertyFound {
				report.MissingPropertyMappings++
			}
			if legacyRoomID != "" && legacyRoomID != "0" && !roomFound {
				report.MissingRoomMappings++
			}
			report.Skipped = append(report.Skipped, JournalSkipRecord{
				LegacyScheduleID: legacyScheduleID,
				LegacyEstateID:   strings.TrimSpace(schedule.EstateID),
				LegacyRoomID:     legacyRoomID,
				Reason:           skipReason,
			})
			continue
		}

		report.EligibleRows++
		report.RepliesMerged += len(normalized.Replies)
		report.PlaceholderAuthorUses++
		report.ClassificationCounts[normalized.TargetTable]++

		if normalized.TargetTable == repairRequestTargetTableName {
			repairRequestID, status, err := insertMigratedRepairRequest(ctx, tx, normalized, placeholderAuthorID)
			if err != nil {
				return nil, err
			}
			if err := insertLegacyScheduleMapping(ctx, tx, normalized.LegacyScheduleID, repairRequestTargetTableName, repairRequestID); err != nil {
				return nil, err
			}
			report.ImportedRepairRequests++
			report.MappingsCreated++
			report.RepairStatusCounts[status]++
			continue
		}

		if isRepairLikeWithoutRoom(normalized) {
			report.NonRoomRepairJournalRows++
		}

		journalLogID, err := insertMigratedJournalLog(ctx, tx, normalized, placeholderAuthorID)
		if err != nil {
			return nil, err
		}
		if err := insertLegacyScheduleMapping(ctx, tx, normalized.LegacyScheduleID, journalTargetTableName, journalLogID); err != nil {
			return nil, err
		}
		report.ImportedJournalLogs++
		report.MappingsCreated++
	}

	report.ClassificationCounts = sortedDistribution(report.ClassificationCounts)
	report.RepairStatusCounts = sortedDistribution(report.RepairStatusCounts)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit journal migration: %w", err)
	}
	committed = true

	reportPath, err := writeJournalMigrationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func loadLegacySchedules(path string) ([]legacyScheduleRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyScheduleSourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyScheduleSourceRootKey)
	}

	var source legacyScheduleSource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode schedule records from %s: %w", path, err)
	}
	if len(source.Schedules) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode schedule records from %s: empty result after parsing %q", path, legacyScheduleSourceRootKey)
	}

	return source.Schedules, nil
}

func loadLegacyReplies(path string) ([]legacyReplyRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source file %s: %w", path, err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode source file %s: %w", path, err)
	}

	rawRecords, ok := payload[legacyReplySourceRootKey]
	if !ok {
		return nil, fmt.Errorf("source file %s missing root key %q", path, legacyReplySourceRootKey)
	}

	var source legacyReplySource
	if err := json.Unmarshal(content, &source); err != nil {
		return nil, fmt.Errorf("decode reply records from %s: %w", path, err)
	}
	if len(source.Replies) == 0 && len(rawRecords) > 0 && string(rawRecords) != "[]" {
		return nil, fmt.Errorf("decode reply records from %s: empty result after parsing %q", path, legacyReplySourceRootKey)
	}

	return source.Replies, nil
}

func buildLegacyReplyIndex(schedules []legacyScheduleRecord, replies []legacyReplyRecord) (map[string][]normalizedLegacyReply, int) {
	scheduleIDs := make(map[string]struct{}, len(schedules))
	for _, schedule := range schedules {
		scheduleIDs[strings.TrimSpace(schedule.ScheduleID)] = struct{}{}
	}

	index := make(map[string][]normalizedLegacyReply)
	orphanReplyRows := 0

	for _, reply := range replies {
		scheduleID := strings.TrimSpace(reply.ScheduleID)
		if _, ok := scheduleIDs[scheduleID]; !ok {
			orphanReplyRows++
			continue
		}

		replyTime, err := parseLegacyJournalTimestamp(reply.Date, legacyReplyTimestampLayout)
		if err != nil {
			orphanReplyRows++
			continue
		}

		index[scheduleID] = append(index[scheduleID], normalizedLegacyReply{
			LegacyReplyID: strings.TrimSpace(reply.ReplyID),
			LegacyUID:     strings.TrimSpace(reply.LegacyUID),
			CreatedAt:     replyTime,
			Content:       normalizeLegacyRichText(reply.Content),
		})
	}

	for scheduleID := range index {
		sort.Slice(index[scheduleID], func(i, j int) bool {
			left := index[scheduleID][i]
			right := index[scheduleID][j]
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
			return left.LegacyReplyID < right.LegacyReplyID
		})
	}

	return index, orphanReplyRows
}

func normalizeJournalSchedule(
	record legacyScheduleRecord,
	propertyID string,
	propertyFound bool,
	roomID *string,
	roomFound bool,
	replies []normalizedLegacyReply,
) (normalizedJournalSchedule, string, error) {
	normalized := normalizedJournalSchedule{
		LegacyScheduleID: strings.TrimSpace(record.ScheduleID),
		LegacyEstateID:   strings.TrimSpace(record.EstateID),
		LegacyRoomID:     strings.TrimSpace(record.RoomID),
		PropertyID:       propertyID,
		RoomID:           roomID,
		Kind:             normalizeLegacyPlainText(record.Kind),
		Status:           normalizeLegacyPlainText(record.Status),
		Facility:         normalizeLegacyPlainText(record.Facility),
		Content:          normalizeLegacyRichText(record.Content),
		LegacyUID:        strings.TrimSpace(record.LegacyUID),
		AssistUID:        strings.TrimSpace(record.AssistUID),
		Replies:          replies,
	}

	switch {
	case normalized.LegacyScheduleID == "":
		return normalizedJournalSchedule{}, "missing estate_schedule_id", nil
	case normalized.LegacyEstateID == "":
		return normalizedJournalSchedule{}, "missing estate_id", nil
	case !propertyFound:
		return normalizedJournalSchedule{}, "missing property mapping for estate_id", nil
	}

	if normalized.LegacyRoomID != "" && normalized.LegacyRoomID != "0" && !roomFound {
		return normalizedJournalSchedule{}, "missing room mapping for estate_room_id", nil
	}

	if normalized.Content == "" && len(normalized.Replies) == 0 {
		return normalizedJournalSchedule{}, "missing schedule content and replies", nil
	}

	createdAt, err := parseLegacyJournalTimestamp(record.Date, legacyScheduleTimestampLayout)
	if err != nil {
		return normalizedJournalSchedule{}, fmt.Sprintf("invalid estate_schedule_date: %v", err), nil
	}
	normalized.CreatedAt = createdAt

	if shouldClassifyAsRepairRequest(normalized) {
		normalized.TargetTable = repairRequestTargetTableName
	} else {
		normalized.TargetTable = journalTargetTableName
	}

	return normalized, "", nil
}

func parseLegacyJournalTimestamp(value string, layout string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, errors.New("empty timestamp")
	}

	parsed, err := time.ParseInLocation(layout, trimmed, legacyBillTimestampLocation)
	if err != nil {
		return time.Time{}, err
	}

	return parsed.UTC(), nil
}

func shouldClassifyAsRepairRequest(schedule normalizedJournalSchedule) bool {
	if schedule.RoomID == nil {
		return false
	}

	if _, ok := repairKinds[schedule.Kind]; ok {
		return true
	}

	searchSpace := strings.ToLower(strings.Join([]string{
		schedule.Kind,
		schedule.Status,
		schedule.Facility,
		schedule.Content,
	}, "\n"))

	for _, keyword := range repairSignalKeywords {
		if strings.Contains(searchSpace, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

func isRepairLikeWithoutRoom(schedule normalizedJournalSchedule) bool {
	if schedule.RoomID != nil {
		return false
	}

	if _, ok := repairKinds[schedule.Kind]; ok {
		return true
	}

	searchSpace := strings.ToLower(strings.Join([]string{
		schedule.Kind,
		schedule.Status,
		schedule.Facility,
		schedule.Content,
	}, "\n"))

	for _, keyword := range repairSignalKeywords {
		if strings.Contains(searchSpace, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

func deriveRepairRequestStatus(legacyStatus string) string {
	switch strings.TrimSpace(legacyStatus) {
	case "完成":
		return repairRequestStatusCompleted
	case "取消":
		return repairRequestStatusCancelled
	default:
		return repairRequestStatusSubmitted
	}
}

func buildLegacyScheduleBody(schedule normalizedJournalSchedule) string {
	sections := make([]string, 0, 3)
	if schedule.Content != "" {
		sections = append(sections, schedule.Content)
	}

	metadataLines := []string{
		fmt.Sprintf("[Legacy schedule metadata] id=%s kind=%s status=%s author_uid=%s assist_uid=%s facility=%s room_id=%s estate_id=%s",
			schedule.LegacyScheduleID,
			fallbackMetadataValue(schedule.Kind),
			fallbackMetadataValue(schedule.Status),
			fallbackMetadataValue(schedule.LegacyUID),
			fallbackMetadataValue(schedule.AssistUID),
			fallbackMetadataValue(schedule.Facility),
			fallbackMetadataValue(schedule.LegacyRoomID),
			fallbackMetadataValue(schedule.LegacyEstateID),
		),
	}
	sections = append(sections, strings.Join(metadataLines, "\n"))

	if len(schedule.Replies) > 0 {
		replyLines := make([]string, 0, len(schedule.Replies)+1)
		replyLines = append(replyLines, "[Legacy replies]")
		for _, reply := range schedule.Replies {
			content := reply.Content
			if content == "" {
				content = "<empty>"
			}
			replyLines = append(replyLines, fmt.Sprintf("- %s reply_id=%s uid=%s %s",
				reply.CreatedAt.Format(time.RFC3339),
				reply.LegacyReplyID,
				fallbackMetadataValue(reply.LegacyUID),
				content,
			))
		}
		sections = append(sections, strings.Join(replyLines, "\n"))
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func fallbackMetadataValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "<empty>"
	}

	return trimmed
}

func deriveRepairRequestTitle(schedule normalizedJournalSchedule) string {
	firstLine := strings.TrimSpace(schedule.Content)
	if idx := strings.Index(firstLine, "\n"); idx >= 0 {
		firstLine = strings.TrimSpace(firstLine[:idx])
	}

	title := firstLine
	if schedule.Facility != "" {
		switch {
		case title == "":
			title = schedule.Facility
		case !strings.Contains(title, schedule.Facility):
			title = schedule.Facility + " " + title
		}
	}
	if title == "" {
		title = "Legacy repair request " + schedule.LegacyScheduleID
	}
	if len(title) > 200 {
		title = strings.TrimSpace(title[:200])
	}

	return title
}

func ensureLegacyScheduleMappingsTable(ctx context.Context, tx *sql.Tx) error {
	const query = `
CREATE TABLE IF NOT EXISTS legacy_schedule_mappings (
	legacy_schedule_id VARCHAR(50) PRIMARY KEY,
	target_table VARCHAR(50) NOT NULL CHECK (target_table IN ('journal_logs', 'repair_requests')),
	target_id UUID NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`

	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure legacy schedule mappings table: %w", err)
	}

	return nil
}

func ensureJournalPlaceholderAuthor(ctx context.Context, tx *sql.Tx) (string, bool, error) {
	const selectQuery = `
SELECT id
FROM users
WHERE firebase_uid = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var userID string
	if err := tx.QueryRowContext(ctx, selectQuery, legacyJournalPlaceholderUID).Scan(&userID); err == nil {
		return userID, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("query journal placeholder author: %w", err)
	}

	const insertQuery = `
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, 'staff')
RETURNING id
`

	if err := tx.QueryRowContext(
		ctx,
		insertQuery,
		legacyJournalPlaceholderUID,
		legacyJournalPlaceholderEmail,
		legacyJournalPlaceholderName,
	).Scan(&userID); err != nil {
		return "", false, fmt.Errorf("create journal placeholder author: %w", err)
	}

	return userID, true, nil
}

func journalScheduleMappingExists(ctx context.Context, tx *sql.Tx, legacyScheduleID string) (bool, error) {
	const query = `
SELECT 1
FROM legacy_schedule_mappings
WHERE legacy_schedule_id = $1
LIMIT 1
`

	var exists int
	err := tx.QueryRowContext(ctx, query, legacyScheduleID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check legacy schedule mapping %s: %w", legacyScheduleID, err)
	}

	return true, nil
}

func findRoomIDByLegacyRoomID(ctx context.Context, tx *sql.Tx, legacyRoomID string) (string, bool, error) {
	const query = `
SELECT room_id
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`

	var roomID string
	err := tx.QueryRowContext(ctx, query, legacyRoomID).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query room mapping %s: %w", legacyRoomID, err)
	}

	return roomID, true, nil
}

func insertMigratedJournalLog(ctx context.Context, tx *sql.Tx, schedule normalizedJournalSchedule, authorID string) (string, error) {
	const query = `
INSERT INTO journal_logs (
	property_id,
	room_id,
	author_id,
	content,
	created_at,
	updated_at
) VALUES ($1, $2, $3, $4, $5, $5)
RETURNING id
`

	var roomArg interface{}
	if schedule.RoomID != nil {
		roomArg = *schedule.RoomID
	}

	var journalLogID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		schedule.PropertyID,
		roomArg,
		authorID,
		buildLegacyScheduleBody(schedule),
		schedule.CreatedAt,
	).Scan(&journalLogID); err != nil {
		return "", fmt.Errorf("insert journal log for legacy schedule %s: %w", schedule.LegacyScheduleID, err)
	}

	return journalLogID, nil
}

func insertMigratedRepairRequest(ctx context.Context, tx *sql.Tx, schedule normalizedJournalSchedule, authorID string) (string, string, error) {
	const query = `
INSERT INTO repair_requests (
	property_id,
	room_id,
	submitted_by,
	title,
	description,
	status,
	submitted_at,
	completed_at,
	created_at,
	updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7, $7)
RETURNING id
`

	status := deriveRepairRequestStatus(schedule.Status)
	var completedAt interface{}
	if status == repairRequestStatusCompleted {
		completedAt = schedule.CreatedAt
	}

	var repairRequestID string
	if err := tx.QueryRowContext(
		ctx,
		query,
		schedule.PropertyID,
		*schedule.RoomID,
		authorID,
		deriveRepairRequestTitle(schedule),
		buildLegacyScheduleBody(schedule),
		status,
		schedule.CreatedAt,
		completedAt,
	).Scan(&repairRequestID); err != nil {
		return "", "", fmt.Errorf("insert repair request for legacy schedule %s: %w", schedule.LegacyScheduleID, err)
	}

	return repairRequestID, status, nil
}

func insertLegacyScheduleMapping(ctx context.Context, tx *sql.Tx, legacyScheduleID string, targetTable string, targetID string) error {
	const query = `
INSERT INTO legacy_schedule_mappings (
	legacy_schedule_id,
	target_table,
	target_id
) VALUES ($1, $2, $3)
`

	if _, err := tx.ExecContext(ctx, query, legacyScheduleID, targetTable, targetID); err != nil {
		return fmt.Errorf("insert legacy schedule mapping %s: %w", legacyScheduleID, err)
	}

	return nil
}

func writeJournalMigrationReport(reportDir string, report *JournalMigrationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", reportDir, err)
	}

	path := filepath.Join(reportDir, task12ReportFileName)
	report.ReportPath = path

	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal journal migration report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write journal migration report %s: %w", path, err)
	}

	return path, nil
}
