package core

import (
	"errors"
	"time"
)

// Role defines operator permissions.
type Role string

const (
	RoleAdmin    Role = "ADMIN"
	RoleAgent    Role = "AGENT"
	RoleReadonly Role = "READONLY"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserInactive = errors.New("user account is inactive")
)

// User represents an operator, staff member, or technician.
type User struct {
	ID        int64     `json:"id"`
	UUID      string    `json:"uuid"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Role      Role      `json:"role"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// CanTriage checks if the user has permission to modify tickets.
func (u User) CanTriage() bool {
	return u.IsActive && (u.Role == RoleAdmin || u.Role == RoleAgent)
}

// IsAdmin checks if the user has administrative privileges.
func (u User) IsAdmin() bool {
	return u.IsActive && u.Role == RoleAdmin
}
