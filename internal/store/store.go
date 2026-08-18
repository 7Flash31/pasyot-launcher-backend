package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"time"

	"pasyot-launcher/internal/domain"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := addMissingColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	if err := dropUnusedColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func dropUnusedColumns(db *sql.DB) error {
	columns := []struct{ table, column string }{
		{"modpacks", "name"},
	}
	for _, c := range columns {
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&exists)
		if err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE ` + c.table + ` DROP COLUMN ` + c.column); err != nil {
			return fmt.Errorf("%s.%s: %w", c.table, c.column, err)
		}
		log.Printf("schema: dropped column %s.%s", c.table, c.column)
	}
	return nil
}

func addMissingColumns(db *sql.DB) error {
	columns := []struct{ table, column, ddl string }{
		{"modpacks", "loader", `ALTER TABLE modpacks ADD COLUMN loader TEXT NOT NULL DEFAULT ''`},
		{"modpacks", "minecraft", `ALTER TABLE modpacks ADD COLUMN minecraft TEXT NOT NULL DEFAULT ''`},
	}
	for _, c := range columns {
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := db.Exec(c.ddl); err != nil {
			return fmt.Errorf("%s.%s: %w", c.table, c.column, err)
		}
		log.Printf("schema: added column %s.%s", c.table, c.column)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) UpsertUser(ctx context.Context, id, sub, username, email, avatar string, admin bool) (*domain.User, error) {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, vedrow_sub, username, email, avatar_url, is_admin, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (vedrow_sub) DO UPDATE SET
			username   = excluded.username,
			email      = excluded.email,
			avatar_url = excluded.avatar_url,
			is_admin   = MAX(users.is_admin, excluded.is_admin)`,
		id, sub, username, email, avatar, boolInt(admin), now)
	if err != nil {
		return nil, err
	}
	return s.userBy(ctx, "vedrow_sub", sub)
}

func (s *Store) UserByID(ctx context.Context, id string) (*domain.User, error) {
	return s.userBy(ctx, "id", id)
}

func (s *Store) userBy(ctx context.Context, column, value string) (*domain.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, vedrow_sub, username, email, avatar_url, is_admin, created_at
		FROM users WHERE `+column+` = ?`, value)
	return scanUser(row)
}

func (s *Store) UserBySession(ctx context.Context, tokenHash string) (*domain.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.vedrow_sub, u.username, u.email, u.avatar_url, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, time.Now().Unix())
	return scanUser(row)
}

func scanUser(row *sql.Row) (*domain.User, error) {
	var u domain.User
	var admin int
	var created int64
	if err := row.Scan(&u.ID, &u.VedrowSub, &u.Username, &u.Email, &u.AvatarURL, &admin, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.IsAdmin = admin != 0
	u.CreatedAt = time.Unix(created, 0).UTC()
	return &u, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, now.Add(ttl).Unix(), now.Unix())
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) SaveLoginState(ctx context.Context, state, verifier, next string, ttl time.Duration) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM login_states WHERE expires_at < ?`, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO login_states (state, verifier, next, expires_at) VALUES (?, ?, ?, ?)`,
		state, verifier, next, time.Now().Add(ttl).Unix())
	return err
}

func (s *Store) TakeLoginState(ctx context.Context, state string) (verifier, next string, err error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT verifier, next FROM login_states WHERE state = ? AND expires_at > ?`,
		state, time.Now().Unix())
	if err := row.Scan(&verifier, &next); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM login_states WHERE state = ?`, state); err != nil {
		return "", "", err
	}
	return verifier, next, nil
}

func (s *Store) CreateModpack(ctx context.Context, name, description, loader, minecraft string) (*domain.Modpack, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO modpacks (slug, description, loader, minecraft, created_at) VALUES (?, ?, ?, ?, ?)`,
		name, description, loader, minecraft, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return s.Modpack(ctx, name)
}

func (s *Store) UpdateModpack(ctx context.Context, name, description, loader, minecraft string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE modpacks SET description = ?, loader = ?, minecraft = ? WHERE slug = ?`,
		description, loader, minecraft, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteModpack(ctx context.Context, slug string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	dropped, err := txStrings(ctx, tx, `SELECT DISTINCT sha256 FROM version_files WHERE modpack_slug = ?`, slug)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM modpacks WHERE slug = ?`, slug)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	orphans, err := txOrphans(ctx, tx, dropped)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return orphans, nil
}

const modpackColumns = `
	m.slug AS name, m.description, m.loader, m.minecraft, m.created_at,
	COALESCE((SELECT MAX(number) FROM versions v WHERE v.modpack_slug = m.slug), 0)`

func (s *Store) Modpacks(ctx context.Context) ([]domain.Modpack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+modpackColumns+` FROM modpacks m ORDER BY m.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.Modpack{}
	for rows.Next() {
		var m domain.Modpack
		var created int64
		if err := rows.Scan(&m.Name, &m.Description, &m.Loader, &m.Minecraft, &created, &m.LatestVersion); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(created, 0).UTC()
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) Modpack(ctx context.Context, slug string) (*domain.Modpack, error) {
	var m domain.Modpack
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT `+modpackColumns+` FROM modpacks m WHERE m.slug = ?`, slug).
		Scan(&m.Name, &m.Description, &m.Loader, &m.Minecraft, &created, &m.LatestVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.Unix(created, 0).UTC()
	return &m, nil
}

func (s *Store) ReplaceVersion(ctx context.Context, slug string, files []domain.File, notes, createdBy, fingerprint string) (*domain.Version, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var number int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM versions WHERE modpack_slug = ?`, slug).Scan(&number); err != nil {
		return nil, nil, err
	}

	dropped, err := txStrings(ctx, tx, `SELECT DISTINCT sha256 FROM version_files WHERE modpack_slug = ?`, slug)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM versions WHERE modpack_slug = ?`, slug); err != nil {
		return nil, nil, err
	}

	var total int64
	for _, f := range files {
		total += f.Size
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO versions (modpack_slug, number, notes, file_count, total_bytes, fingerprint, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		slug, number, notes, len(files), total, fingerprint, createdBy, now); err != nil {
		return nil, nil, err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO version_files (modpack_slug, number, path, grp, size, sha256, optional)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, nil, err
	}
	defer stmt.Close()
	for _, f := range files {
		if _, err := stmt.ExecContext(ctx, slug, number, f.Path, f.Group, f.Size, f.SHA256, boolInt(f.Optional)); err != nil {
			return nil, nil, err
		}
	}

	orphans, err := txOrphans(ctx, tx, dropped)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &domain.Version{
		Version: number, Notes: notes, FileCount: len(files), TotalBytes: total,
		CreatedBy: createdBy, CreatedAt: time.Unix(now, 0).UTC(),
	}, orphans, nil
}

func txStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func txOrphans(ctx context.Context, tx *sql.Tx, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	referenced := map[string]bool{}
	for _, query := range []string{
		`SELECT DISTINCT sha256 FROM version_files`,
		`SELECT DISTINCT sha256 FROM launcher_builds`,
	} {
		list, err := txStrings(ctx, tx, query)
		if err != nil {
			return nil, err
		}
		for _, sha := range list {
			referenced[sha] = true
		}
	}
	var orphans []string
	for _, sha := range candidates {
		if !referenced[sha] {
			orphans = append(orphans, sha)
		}
	}
	return orphans, nil
}

func (s *Store) Versions(ctx context.Context, slug string) ([]domain.Version, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT number, notes, file_count, total_bytes, created_by, created_at
		FROM versions WHERE modpack_slug = ? ORDER BY number DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *v)
	}
	return list, rows.Err()
}

func (s *Store) Version(ctx context.Context, slug string, number int) (*domain.Version, error) {
	q := `SELECT number, notes, file_count, total_bytes, created_by, created_at
	      FROM versions WHERE modpack_slug = ? AND number = ?`
	args := []any{slug, number}
	if number == 0 {
		q = `SELECT number, notes, file_count, total_bytes, created_by, created_at
		     FROM versions WHERE modpack_slug = ? ORDER BY number DESC LIMIT 1`
		args = []any{slug}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanVersion(rows)
}

func scanVersion(rows *sql.Rows) (*domain.Version, error) {
	var v domain.Version
	var created int64
	if err := rows.Scan(&v.Version, &v.Notes, &v.FileCount, &v.TotalBytes, &v.CreatedBy, &created); err != nil {
		return nil, err
	}
	v.CreatedAt = time.Unix(created, 0).UTC()
	return &v, nil
}

func (s *Store) LatestFingerprint(ctx context.Context, slug string) (fingerprint string, number int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT fingerprint, number FROM versions WHERE modpack_slug = ?
		ORDER BY number DESC LIMIT 1`, slug).Scan(&fingerprint, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return fingerprint, number, err
}

func (s *Store) VersionFiles(ctx context.Context, slug string, number int) ([]domain.File, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, grp, size, sha256, optional FROM version_files
		WHERE modpack_slug = ? AND number = ? ORDER BY path`, slug, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []domain.File{}
	for rows.Next() {
		var f domain.File
		var optional int
		if err := rows.Scan(&f.Path, &f.Group, &f.Size, &f.SHA256, &optional); err != nil {
			return nil, err
		}
		f.Optional = optional != 0
		list = append(list, f)
	}
	return list, rows.Err()
}

func (s *Store) SaveLauncherBuild(ctx context.Context, b *domain.LauncherBuild) error {
	now := time.Now().UTC().Truncate(time.Second)
	b.CreatedAt = &now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO launcher_builds (version, filename, size, sha256, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (version) DO UPDATE SET
			filename = excluded.filename, size = excluded.size,
			sha256 = excluded.sha256, created_at = excluded.created_at`,
		b.Version, b.Filename, b.Size, b.SHA256, b.CreatedAt.Unix())
	return err
}

func (s *Store) LatestLauncherBuild(ctx context.Context) (*domain.LauncherBuild, error) {
	var b domain.LauncherBuild
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT version, filename, size, sha256, created_at FROM launcher_builds
		ORDER BY created_at DESC LIMIT 1`).Scan(&b.Version, &b.Filename, &b.Size, &b.SHA256, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	at := time.Unix(created, 0).UTC()
	b.CreatedAt = &at
	return &b, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
