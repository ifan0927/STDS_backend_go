package database

func NotDeleted(column string) string {
	if column == "" {
		column = "deleted_at"
	}

	return column + " IS NULL"
}
