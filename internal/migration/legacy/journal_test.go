package legacy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestShouldClassifyAsRepairRequest(t *testing.T) {
	roomID := "room-1"

	tests := []struct {
		name     string
		schedule normalizedJournalSchedule
		want     bool
	}{
		{
			name: "repair kind with room becomes repair request",
			schedule: normalizedJournalSchedule{
				RoomID:  &roomID,
				Kind:    "叫修",
				Content: "洗衣機維修",
			},
			want: true,
		},
		{
			name: "room-specific repair language becomes repair request",
			schedule: normalizedJournalSchedule{
				RoomID:  &roomID,
				Kind:    "例行",
				Content: "冷氣漏水，維修外管。",
			},
			want: true,
		},
		{
			name: "non-room repair stays journal",
			schedule: normalizedJournalSchedule{
				Kind:    "叫修",
				Content: "杜先生下午過來維修",
			},
			want: false,
		},
		{
			name: "room-specific finance note stays journal",
			schedule: normalizedJournalSchedule{
				RoomID:  &roomID,
				Kind:    "例行",
				Content: "房租收入 $5000",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldClassifyAsRepairRequest(tt.schedule); got != tt.want {
				t.Fatalf("shouldClassifyAsRepairRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildLegacyScheduleBodyMergesRepliesAndMetadata(t *testing.T) {
	body := buildLegacyScheduleBody(normalizedJournalSchedule{
		LegacyScheduleID: "33",
		LegacyEstateID:   "1",
		LegacyRoomID:     "44",
		Kind:             "叫修",
		Status:           "完成",
		Facility:         "冷氣",
		Content:          "冷氣漏水",
		LegacyUID:        "2242",
		AssistUID:        "1",
		Replies: []normalizedLegacyReply{
			{
				LegacyReplyID: "8",
				LegacyUID:     "1",
				CreatedAt:     time.Date(2018, 10, 24, 4, 41, 54, 0, time.UTC),
				Content:       "改下周處理",
			},
		},
	})

	if !strings.Contains(body, "Legacy schedule metadata") {
		t.Fatal("body should contain legacy schedule metadata")
	}
	if !strings.Contains(body, "author_uid=2242") {
		t.Fatal("body should preserve legacy author uid")
	}
	if !strings.Contains(body, "reply_id=8 uid=1") {
		t.Fatal("body should preserve legacy reply uid")
	}
	if !strings.Contains(body, "冷氣漏水") || !strings.Contains(body, "改下周處理") {
		t.Fatal("body should contain schedule and reply content")
	}
}

func TestMigrateJournalWritesRepairAndJournalRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sourceDir := t.TempDir()
	reportDir := t.TempDir()

	writeLegacyScheduleFixture(t, sourceDir, []legacyScheduleRecord{
		{
			ScheduleID: "16",
			RoomID:     "10",
			Date:       "2018-10-15 10:57:00",
			Kind:       "叫修",
			LegacyUID:  "2242",
			Facility:   "洗衣機",
			Content:    "3F脫水機維修",
			AssistUID:  "0",
			Status:     "完成",
			EstateID:   "1",
		},
		{
			ScheduleID: "17",
			RoomID:     "0",
			Date:       "2018-10-15 11:00:00",
			Kind:       "叫修",
			LegacyUID:  "2242",
			Facility:   "洗衣機",
			Content:    "杜先生下午會過來維修",
			AssistUID:  "0",
			Status:     "公告",
			EstateID:   "1",
		},
		{
			ScheduleID: "18",
			RoomID:     "11",
			Date:       "2018-10-15 12:00:00",
			Kind:       "例行",
			LegacyUID:  "1",
			Content:    "房租收入 $5000",
			AssistUID:  "-1",
			Status:     "",
			EstateID:   "1",
		},
	})
	writeLegacyReplyFixture(t, sourceDir, []legacyReplyRecord{
		{
			ReplyID:    "2",
			ScheduleID: "16",
			Date:       "2018-10-15 11:30:00",
			Content:    "已處理",
			LegacyUID:  "1",
		},
	})

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
CREATE TABLE IF NOT EXISTS legacy_schedule_mappings (
	legacy_schedule_id VARCHAR(50) PRIMARY KEY,
	target_table VARCHAR(50) NOT NULL CHECK (target_table IN ('journal_logs', 'repair_requests')),
	target_id UUID NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
`)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM users
WHERE firebase_uid = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs(legacyJournalPlaceholderUID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role
) VALUES ($1, $2, $3, 'staff')
RETURNING id
`)).
		WithArgs(legacyJournalPlaceholderUID, legacyJournalPlaceholderEmail, legacyJournalPlaceholderName).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("user-placeholder-1"))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_schedule_mappings
WHERE legacy_schedule_id = $1
LIMIT 1
`)).
		WithArgs("16").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT room_id
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`)).
		WithArgs("10").
		WillReturnRows(sqlmock.NewRows([]string{"room_id"}).AddRow("room-10"))
	mock.ExpectQuery(regexp.QuoteMeta(`
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
`)).
		WithArgs(
			"property-1",
			"room-10",
			"user-placeholder-1",
			"洗衣機 3F脫水機維修",
			sqlmock.AnyArg(),
			repairRequestStatusCompleted,
			time.Date(2018, 10, 15, 2, 57, 0, 0, time.UTC),
			time.Date(2018, 10, 15, 2, 57, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("repair-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_schedule_mappings (
	legacy_schedule_id,
	target_table,
	target_id
) VALUES ($1, $2, $3)
`)).
		WithArgs("16", repairRequestTargetTableName, "repair-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_schedule_mappings
WHERE legacy_schedule_id = $1
LIMIT 1
`)).
		WithArgs("17").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO journal_logs (
	property_id,
	room_id,
	author_id,
	content,
	created_at,
	updated_at
) VALUES ($1, $2, $3, $4, $5, $5)
RETURNING id
`)).
		WithArgs(
			"property-1",
			nil,
			"user-placeholder-1",
			sqlmock.AnyArg(),
			time.Date(2018, 10, 15, 3, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("journal-1"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_schedule_mappings (
	legacy_schedule_id,
	target_table,
	target_id
) VALUES ($1, $2, $3)
`)).
		WithArgs("17", journalTargetTableName, "journal-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT 1
FROM legacy_schedule_mappings
WHERE legacy_schedule_id = $1
LIMIT 1
`)).
		WithArgs("18").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM legacy_property_mappings
WHERE legacy_estate_id = $1
LIMIT 1
`)).
		WithArgs("1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT room_id
FROM legacy_room_mappings
WHERE legacy_room_id = $1
LIMIT 1
`)).
		WithArgs("11").
		WillReturnRows(sqlmock.NewRows([]string{"room_id"}).AddRow("room-11"))
	mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO journal_logs (
	property_id,
	room_id,
	author_id,
	content,
	created_at,
	updated_at
) VALUES ($1, $2, $3, $4, $5, $5)
RETURNING id
`)).
		WithArgs(
			"property-1",
			"room-11",
			"user-placeholder-1",
			sqlmock.AnyArg(),
			time.Date(2018, 10, 15, 4, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("journal-2"))
	mock.ExpectExec(regexp.QuoteMeta(`
INSERT INTO legacy_schedule_mappings (
	legacy_schedule_id,
	target_table,
	target_id
) VALUES ($1, $2, $3)
`)).
		WithArgs("18", journalTargetTableName, "journal-2").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	report, err := MigrateJournal(context.Background(), db, MigrateJournalOptions{
		SourceDir: sourceDir,
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("MigrateJournal() error = %v", err)
	}

	if report.ImportedRepairRequests != 1 {
		t.Fatalf("ImportedRepairRequests = %d, want 1", report.ImportedRepairRequests)
	}
	if report.ImportedJournalLogs != 2 {
		t.Fatalf("ImportedJournalLogs = %d, want 2", report.ImportedJournalLogs)
	}
	if report.NonRoomRepairJournalRows != 1 {
		t.Fatalf("NonRoomRepairJournalRows = %d, want 1", report.NonRoomRepairJournalRows)
	}
	if report.RepliesMerged != 1 {
		t.Fatalf("RepliesMerged = %d, want 1", report.RepliesMerged)
	}
	if report.PlaceholderAuthorUses != 3 {
		t.Fatalf("PlaceholderAuthorUses = %d, want 3", report.PlaceholderAuthorUses)
	}
	if report.RepairStatusCounts[repairRequestStatusCompleted] != 1 {
		t.Fatalf("RepairStatusCounts[completed] = %d, want 1", report.RepairStatusCounts[repairRequestStatusCompleted])
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted JournalMigrationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.ImportedJournalLogs != 2 || persisted.ImportedRepairRequests != 1 {
		t.Fatalf("persisted report counts = %+v", persisted)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func writeLegacyScheduleFixture(t *testing.T, sourceDir string, schedules []legacyScheduleRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyScheduleSourceRootKey: schedules,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(sourceDir, legacyScheduleSourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func writeLegacyReplyFixture(t *testing.T, sourceDir string, replies []legacyReplyRecord) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		legacyReplySourceRootKey: replies,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	path := filepath.Join(sourceDir, legacyReplySourceFileName)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}
