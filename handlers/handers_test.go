package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-fuego/fuego"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"gorm/handlers"
	"gorm/models"
)

// MockUserQueries implements UserQueryInterface for testing
type MockUserQueries struct {
	users        map[uint]*models.User
	returnErr    error
	existingUser *models.User
}

func (m *MockUserQueries) GetUsers() ([]models.User, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	var users []models.User
	for _, u := range m.users {
		users = append(users, *u)
	}
	return users, nil
}

func (m *MockUserQueries) GetUserByID(id uint) (*models.User, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	user, exists := m.users[id]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return user, nil
}

func (m *MockUserQueries) GetUserByEmail(email string) (*models.User, error) {
	if m.existingUser != nil {
		return m.existingUser, nil
	}
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *MockUserQueries) CreateUser(user *models.User) error {
	if m.returnErr != nil {
		return m.returnErr
	}
	if m.users == nil {
		m.users = make(map[uint]*models.User)
	}
	user.ID = uint(len(m.users) + 1)
	m.users[user.ID] = user
	return nil
}

func (m *MockUserQueries) UpdateUser(user *models.User) error {
	if m.returnErr != nil {
		return m.returnErr
	}
	if _, exists := m.users[user.ID]; !exists {
		return gorm.ErrRecordNotFound
	}
	m.users[user.ID] = user
	return nil
}

func (m *MockUserQueries) DeleteUser(id uint) error {
	if m.returnErr != nil {
		return m.returnErr
	}
	if _, exists := m.users[id]; !exists {
		return gorm.ErrRecordNotFound
	}
	delete(m.users, id)
	return nil
}

func TestHandlers(t *testing.T) {
	// Common setup
	setupServer := func(mock *MockUserQueries) *fuego.Server {
		h := &handlers.Handlers{
			UserQueries: mock,
		}

		s := fuego.NewServer(
			fuego.WithoutStartupMessages(),
		)

		fuego.Get(s, "/users", h.GetUsers)
		fuego.Get(s, "/users/{id}", h.GetUserByID)
		fuego.Post(s, "/users", h.CreateUser)
		fuego.Put(s, "/users/{id}", h.UpdateUser)
		fuego.Delete(s, "/users/{id}", h.DeleteUser)

		// s.Get("/users", fuego.Handler(h.GetUsers))
		// s.Get("/users/{id}", fuego.Handler(h.GetUserByID))
		// s.Post("/users", fuego.Handler(h.CreateUser))
		// s.Put("/users/{id}", fuego.Handler(h.UpdateUser))
		// s.Delete("/users/{id}", fuego.Handler(h.DeleteUser))

		return s
	}

	t.Run("GetUserByID", func(t *testing.T) {
		tests := []struct {
			name         string
			userID       string
			mockSetup    func(*MockUserQueries)
			wantStatus   int
			wantContains string
		}{
			{
				name:         "invalid ID",
				userID:       "invalid",
				wantStatus:   http.StatusBadRequest,
				wantContains: "Invalid ID",
			},
			{
				name:   "non-existent user",
				userID: "999",
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{
						1: {
							Model: gorm.Model{ID: 1},
							Name:  "Test User",
							Email: "test@example.com",
						},
					}
				},
				wantStatus:   http.StatusNotFound,
				wantContains: "User not found",
			},
			{
				name:   "valid user",
				userID: "1",
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{
						1: {Model: gorm.Model{ID: 1},
							Name:  "Test User",
							Email: "test@example.com"},
					}
				},
				wantStatus:   http.StatusOK,
				wantContains: `"id":1`,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := &MockUserQueries{}
				if tt.mockSetup != nil {
					tt.mockSetup(mock)
				}
				s := setupServer(mock)

				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/users/"+tt.userID, nil)

				s.Mux.ServeHTTP(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
				require.Contains(t, w.Body.String(), tt.wantContains)
			})
		}
	})

	t.Run("CreateUser", func(t *testing.T) {
		tests := []struct {
			name         string
			payload      string
			mockSetup    func(*MockUserQueries)
			wantStatus   int
			wantContains string
		}{
			{
				name:         "valid input",
				payload:      `{"name":"Alice","email":"alice@example.com"}`,
				wantStatus:   http.StatusCreated,
				wantContains: `"id":1`,
			},
			{
				name:         "missing fields",
				payload:      `{"name":""}`,
				wantStatus:   http.StatusBadRequest,
				wantContains: "Missing Required Fields",
			},
			{
				name:    "duplicate email",
				payload: `{"name":"Bob","email":"exists@example.com"}`,
				mockSetup: func(m *MockUserQueries) {
					m.existingUser = &models.User{Email: "exists@example.com"}
				},
				wantStatus:   http.StatusConflict,
				wantContains: "already exists",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := &MockUserQueries{}
				if tt.mockSetup != nil {
					tt.mockSetup(mock)
				}
				s := setupServer(mock)

				w := httptest.NewRecorder()
				req := httptest.NewRequest(
					http.MethodPost,
					"/users",
					strings.NewReader(tt.payload),
				)
				req.Header.Set("Content-Type", "application/json")

				s.Mux.ServeHTTP(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
				require.Contains(t, w.Body.String(), tt.wantContains)
			})
		}
	})

	t.Run("UpdateUser", func(t *testing.T) {
		existingUser := &models.User{
			Model: gorm.Model{ID: 1},
			Name:  "Test User",
			Email: "test@example.com",
		}

		tests := []struct {
			name         string
			userID       string
			payload      string
			mockSetup    func(*MockUserQueries)
			wantStatus   int
			wantContains string
		}{
			{
				name:    "successful update",
				userID:  "1",
				payload: `{"name":"New Name","email":"new@example.com"}`,
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{1: existingUser}
				},
				wantStatus:   http.StatusOK,
				wantContains: `"name":"New Name"`,
			},
			{
				name:         "invalid ID",
				userID:       "invalid",
				payload:      `{"name":"New Name"}`,
				wantStatus:   http.StatusBadRequest,
				wantContains: "Invalid ID",
			},
			{
				name:    "non-existent user",
				userID:  "999",
				payload: `{"name":"New Name"}`,
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{1: existingUser}
				},
				wantStatus:   http.StatusNotFound,
				wantContains: "not found",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := &MockUserQueries{}
				if tt.mockSetup != nil {
					tt.mockSetup(mock)
				}
				s := setupServer(mock)

				w := httptest.NewRecorder()
				req := httptest.NewRequest(
					http.MethodPut,
					"/users/"+tt.userID,
					strings.NewReader(tt.payload),
				)
				req.Header.Set("Content-Type", "application/json")

				s.Mux.ServeHTTP(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
				require.Contains(t, w.Body.String(), tt.wantContains)
			})
		}
	})

	t.Run("DeleteUser", func(t *testing.T) {
		existingUser := &models.User{
			Model: gorm.Model{ID: 1},
			Name:  "Test User",
			Email: "test@example.com",
		}

		tests := []struct {
			name         string
			userID       string
			mockSetup    func(*MockUserQueries)
			wantStatus   int
			wantContains string
		}{
			{
				name:   "successful delete",
				userID: "1",
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{1: existingUser}
				},
				wantStatus: http.StatusOK,
			},
			{
				name:   "non-existent user",
				userID: "999",
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{1: existingUser}
				},
				wantStatus: http.StatusNotFound,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := &MockUserQueries{}
				if tt.mockSetup != nil {
					tt.mockSetup(mock)
				}
				s := setupServer(mock)

				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodDelete, "/users/"+tt.userID, nil)

				s.Mux.ServeHTTP(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
			})
		}
	})

	t.Run("GetUsers", func(t *testing.T) {
		tests := []struct {
			name         string
			mockSetup    func(*MockUserQueries)
			wantStatus   int
			wantContains string
		}{
			{
				name: "empty list",
				mockSetup: func(m *MockUserQueries) {
					m.users = make(map[uint]*models.User)
				},
				wantStatus:   http.StatusOK,
				wantContains: "[]",
			},
			{
				name: "with users",
				mockSetup: func(m *MockUserQueries) {
					m.users = map[uint]*models.User{
						1: {Model: gorm.Model{ID: 1},
							Name:  "Test User",
							Email: "test@example.com"},
						2: {Model: gorm.Model{ID: 1},
							Name:  "Test User",
							Email: "test@example.com"},
					}
				},
				wantStatus:   http.StatusOK,
				wantContains: `"id":1`,
			},
			{
				name: "database error",
				mockSetup: func(m *MockUserQueries) {
					m.returnErr = errors.New("database failure")
				},
				wantStatus: http.StatusInternalServerError,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mock := &MockUserQueries{}
				if tt.mockSetup != nil {
					tt.mockSetup(mock)
				}
				s := setupServer(mock)

				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/users", nil)

				s.Mux.ServeHTTP(w, req)

				require.Equal(t, tt.wantStatus, w.Code)
				if tt.wantContains != "" {
					require.Contains(t, w.Body.String(), tt.wantContains)
				}
			})
		}
	})
}
