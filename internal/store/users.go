package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fluxen/pkg/types"
)

type User struct {
	ID           types.UserID
	OrgID        types.OrgID
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type Users struct {
	pool *pgxpool.Pool
}

func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

// Create inserts the owner account created by the setup flow.
func (u *Users) Create(ctx context.Context, orgID types.OrgID, email, passwordHash, role string) (User, error) {
	var user User
	err := u.pool.QueryRow(ctx, `
		INSERT INTO users (org_id, email, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, org_id, email, password_hash, role, created_at
	`, orgID, email, passwordHash, role).Scan(&user.ID, &user.OrgID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("store: failed to create user: %w", err)
	}
	return user, nil
}

// GetByEmail looks up a user by email across all orgs — login doesn't yet
// know which org a user belongs to, only their email (V1 is single-org
// per deployment, Part E, so in practice this resolves at most one row,
// but the query doesn't assume that).
func (u *Users) GetByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := u.pool.QueryRow(ctx, `
		SELECT id, org_id, email, password_hash, role, created_at
		FROM users
		WHERE email = $1
	`, email).Scan(&user.ID, &user.OrgID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("store: failed to get user: %w", err)
	}
	return user, nil
}

// Get fetches a user by id — used to hydrate the session middleware's
// per-request user/org context.
func (u *Users) Get(ctx context.Context, id types.UserID) (User, error) {
	var user User
	err := u.pool.QueryRow(ctx, `
		SELECT id, org_id, email, password_hash, role, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.OrgID, &user.Email, &user.PasswordHash, &user.Role, &user.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("store: failed to get user: %w", err)
	}
	return user, nil
}
