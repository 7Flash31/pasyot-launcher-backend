package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
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
	return &Store{db: db}, nil
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

func (s *Store) CreateModpack(ctx context.Context, slug, name, description string) (*domain.Modpack, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO modpacks (slug, name, description, created_at) VALUES (?, ?, ?, ?)`,
		slug, name, description, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return s.Modpack(ctx, slug)
}

func (s *Store) DeleteModpack(ctx context.Context, slug string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM modpacks WHERE slug = ?`, slug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const modpackColumns = `
	m.slug, m.name, m.description, m.created_at,
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
		if err := rows.Scan(&m.Slug, &m.Name, &m.Description, &created, &m.LatestVersion); err != nil {
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
		Scan(&m.Slug, &m.Name, &m.Description, &created, &m.LatestVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.Unix(created, 0).UTC()
	return &m, nil
}

func (s *Store) CreateVersion(ctx context.Context, slug string, files []domain.File, notes, createdBy, fingerprint string) (*domain.Version, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var number int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM versions WHERE modpack_slug = ?`, slug).Scan(&number); err != nil {
		return nil, err
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
		return nil, err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO version_files (modpack_slug, number, path, grp, size, sha256, optional)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for _, f := range files {
		if _, err := stmt.ExecContext(ctx, slug, number, f.Path, f.Group, f.Size, f.SHA256, boolInt(f.Optional)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.Version{
		Version: number, Notes: notes, FileCount: len(files), TotalBytes: total,
		CreatedBy: createdBy, CreatedAt: time.Unix(now, 0).UTC(),
	}, nil
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
