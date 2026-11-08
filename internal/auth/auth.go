package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Role defines user permission level.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User represents an authorized user in the system.
type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username,omitempty"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// InviteCode represents a temporary access token for registration.
type InviteCode struct {
	Code      string    `json:"code"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Store holds the persistent list of authorized users and active invite codes.
type Store struct {
	filePath string
	adminID  int64
	mu       sync.RWMutex
	Users    map[int64]*User        `json:"users"`
	Invites  map[string]*InviteCode `json:"invites"`
}

// NewStore loads or initializes the authorization database.
func NewStore(filePath string, defaultAdminID int64) (*Store, error) {
	if filePath == "" {
		filePath = "/opt/superbot/users.json"
	}

	cleanPath := filepath.Clean(filePath)
	s := &Store{
		filePath: cleanPath,
		adminID:  defaultAdminID,
		Users:    make(map[int64]*User),
		Invites:  make(map[string]*InviteCode),
	}

	// Try reading existing file
	if data, err := os.ReadFile(cleanPath); err == nil {
		_ = json.Unmarshal(data, s)
	}

	if s.Users == nil {
		s.Users = make(map[int64]*User)
	}
	if s.Invites == nil {
		s.Invites = make(map[string]*InviteCode)
	}

	// Guarantee default admin is always present and active
	if defaultAdminID > 0 {
		s.Users[defaultAdminID] = &User{
			ID:        defaultAdminID,
			Role:      RoleAdmin,
			CreatedAt: time.Now(),
		}
	}

	_ = s.save()
	return s, nil
}

// IsAuthorized checks if the user is in the whitelist.
func (s *Store) IsAuthorized(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if userID == s.adminID {
		return true
	}
	_, exists := s.Users[userID]
	return exists
}

// IsAdmin checks if the user has administrative privileges.
func (s *Store) IsAdmin(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if userID == s.adminID {
		return true
	}
	u, exists := s.Users[userID]
	return exists && u.Role == RoleAdmin
}

// AddUser adds or updates a user in the whitelist.
func (s *Store) AddUser(id int64, username string, role Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if role == "" {
		role = RoleUser
	}

	s.Users[id] = &User{
		ID:        id,
		Username:  username,
		Role:      role,
		CreatedAt: time.Now(),
	}

	return s.save()
}

// RemoveUser deletes a user from the whitelist.
func (s *Store) RemoveUser(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == s.adminID {
		return fmt.Errorf("нельзя удалить главного администратора системы")
	}

	delete(s.Users, id)
	return s.save()
}

// ListUsers returns all currently registered users.
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*User, 0, len(s.Users))
	for _, u := range s.Users {
		list = append(list, u)
	}
	return list
}

// GenerateInvite creates a 6-character hex invite code valid for a given duration.
func (s *Store) GenerateInvite(adminID int64, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 3) // 6 hex chars, e.g. "a3f89c"
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := hex.EncodeToString(b)

	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	s.Invites[code] = &InviteCode{
		Code:      code,
		CreatedBy: adminID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}

	_ = s.save()
	return code, nil
}

// RedeemInvite validates an invite code and grants the user access.
func (s *Store) RedeemInvite(code string, userID int64, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	inv, exists := s.Invites[code]
	if !exists {
		return fmt.Errorf("инвайт-код не найден или уже использован")
	}

	if time.Now().After(inv.ExpiresAt) {
		delete(s.Invites, code)
		_ = s.save()
		return fmt.Errorf("срок действия инвайт-кода истек")
	}

	// Grant access
	s.Users[userID] = &User{
		ID:        userID,
		Username:  username,
		Role:      RoleUser,
		CreatedAt: time.Now(),
	}

	// Invalidate used code
	delete(s.Invites, code)
	return s.save()
}

func (s *Store) save() error {
	dir := filepath.Dir(s.filePath)
	_ = os.MkdirAll(dir, 0750)

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0600)
}

