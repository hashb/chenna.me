package main

import (
	"database/sql"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

type Entry struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Website   string    `json:"website,omitempty"`
	EntryType string    `json:"entry_type"`
	Content   string    `json:"content,omitempty"`
	ImageData []byte    `json:"-"`
	HasImage  bool      `json:"has_image"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type EntryPage struct {
	Entries      []Entry
	Page         int
	PerPage      int
	TotalEntries int
}

func (p EntryPage) TotalPages() int {
	if p.PerPage <= 0 {
		return 1
	}
	if p.TotalEntries == 0 {
		return 1
	}
	return (p.TotalEntries + p.PerPage - 1) / p.PerPage
}

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA busy_timeout=5000;

		CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			website TEXT DEFAULT '',
			entry_type TEXT NOT NULL CHECK(entry_type IN ('drawing', 'message')),
			content TEXT DEFAULT '',
			image_data BLOB,
			status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'approved', 'rejected')),
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS idx_entries_status ON entries(status);
		CREATE INDEX IF NOT EXISTS idx_entries_status_created ON entries(status, created_at DESC, id DESC);
	`)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func getApprovedEntries(db *sql.DB, page, perPage int) (EntryPage, error) {
	totalEntries, err := countEntriesByStatus(db, "approved")
	if err != nil {
		return EntryPage{}, err
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 1
	}

	totalPages := 1
	if totalEntries > 0 {
		totalPages = (totalEntries + perPage - 1) / perPage
	}
	if page > totalPages {
		page = totalPages
	}

	entries, err := getEntriesByStatus(db, "approved", perPage, (page-1)*perPage, false)
	if err != nil {
		return EntryPage{}, err
	}

	return EntryPage{
		Entries:      entries,
		Page:         page,
		PerPage:      perPage,
		TotalEntries: totalEntries,
	}, nil
}

func getPendingEntries(db *sql.DB) ([]Entry, error) {
	return getEntriesByStatus(db, "pending", 0, 0, true)
}

func countEntriesByStatus(db *sql.DB, status string) (int, error) {
	var total int
	err := db.QueryRow(`SELECT COUNT(*) FROM entries WHERE status = ?`, status).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total, nil
}

func getEntriesByStatus(db *sql.DB, status string, limit, offset int, ascending bool) ([]Entry, error) {
	var query string
	if ascending {
		query = `SELECT id, name, website, entry_type, content, image_data IS NOT NULL, status, created_at
			FROM entries WHERE status = ? ORDER BY created_at ASC, id ASC`
	} else {
		query = `SELECT id, name, website, entry_type, content, image_data IS NOT NULL, status, created_at
			FROM entries WHERE status = ? ORDER BY created_at DESC, id DESC`
	}
	args := []any{status}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var e Entry
		var createdAt string
		err := rows.Scan(&e.ID, &e.Name, &e.Website, &e.EntryType, &e.Content, &e.HasImage, &e.Status, &createdAt)
		if err != nil {
			return nil, err
		}
		parsed, parseErr := time.ParseInLocation("2006-01-02 15:04:05", createdAt, time.UTC)
		if parseErr != nil {
			parsed, parseErr = time.Parse(time.RFC3339, createdAt)
		}
		if parseErr != nil {
			log.Printf("warning: failed to parse created_at %q for entry %d: %v", createdAt, e.ID, parseErr)
		}
		e.CreatedAt = parsed
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []Entry{}
	}
	return entries, rows.Err()
}

func createEntry(db *sql.DB, e *Entry) (int64, error) {
	result, err := db.Exec(`
		INSERT INTO entries (name, website, entry_type, content, image_data)
		VALUES (?, ?, ?, ?, ?)
	`, e.Name, e.Website, e.EntryType, e.Content, e.ImageData)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func getEntryImage(db *sql.DB, id int64, status string) ([]byte, error) {
	query := `SELECT image_data FROM entries WHERE id = ?`
	args := []any{id}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	var data []byte
	err := db.QueryRow(query, args...).Scan(&data)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, sql.ErrNoRows
	}
	return data, nil
}

func approveEntry(db *sql.DB, id int64) error {
	result, err := db.Exec(`UPDATE entries SET status = 'approved' WHERE id = ? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func rejectEntry(db *sql.DB, id int64) error {
	result, err := db.Exec(`UPDATE entries SET status = 'rejected' WHERE id = ? AND status = 'pending'`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func deleteEntry(db *sql.DB, id int64) error {
	result, err := db.Exec(`DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func purgeRejectedEntries(db *sql.DB) (int64, error) {
	result, err := db.Exec(`DELETE FROM entries WHERE status = 'rejected'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
