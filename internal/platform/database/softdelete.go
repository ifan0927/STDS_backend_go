package database

// NotDeleted returns a SQL predicate that matches rows whose soft-delete column
// is still NULL.
func NotDeleted(column string) string {
	if column == "" {
		column = "deleted_at"
	}

	return column + " IS NULL"
}
