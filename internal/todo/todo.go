// Package todo defines the core Todo domain model and its validation rules.
package todo

import (
	"errors"
	"strings"
	"time"
)

const (
	MaxTitleLength       = 200
	MaxDescriptionLength = 2000
)

var (
	ErrNotFound           = errors.New("todo not found")
	ErrTitleRequired      = errors.New("title is required")
	ErrTitleTooLong       = errors.New("title must be at most 200 characters")
	ErrDescriptionTooLong = errors.New("description must be at most 2000 characters")
	ErrInvalidPriority    = errors.New("priority must be one of: low, medium, high")
)

// Priority indicates how important a todo is.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

// Valid reports whether p is a known priority.
func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh:
		return true
	}
	return false
}

// Todo is a single task.
type Todo struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Completed   bool       `json:"completed"`
	Priority    Priority   `json:"priority"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// CreateInput holds the fields accepted when creating a todo.
type CreateInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Priority    Priority   `json:"priority"`
	DueDate     *time.Time `json:"due_date"`
}

// Normalize trims whitespace and applies defaults.
func (in *CreateInput) Normalize() {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if in.Priority == "" {
		in.Priority = PriorityMedium
	}
}

// Validate checks the input against the domain rules.
func (in CreateInput) Validate() error {
	if err := validateTitle(in.Title); err != nil {
		return err
	}
	if len(in.Description) > MaxDescriptionLength {
		return ErrDescriptionTooLong
	}
	if !in.Priority.Valid() {
		return ErrInvalidPriority
	}
	return nil
}

// UpdateInput holds the fields accepted when partially updating a todo.
// Nil fields are left unchanged.
type UpdateInput struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	Completed   *bool      `json:"completed"`
	Priority    *Priority  `json:"priority"`
	DueDate     *time.Time `json:"due_date"`
	// ClearDueDate removes the due date when true.
	ClearDueDate bool `json:"clear_due_date"`
}

// Normalize trims whitespace on provided string fields.
func (in *UpdateInput) Normalize() {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		in.Description = &d
	}
}

// Validate checks the provided fields against the domain rules.
func (in UpdateInput) Validate() error {
	if in.Title != nil {
		if err := validateTitle(*in.Title); err != nil {
			return err
		}
	}
	if in.Description != nil && len(*in.Description) > MaxDescriptionLength {
		return ErrDescriptionTooLong
	}
	if in.Priority != nil && !in.Priority.Valid() {
		return ErrInvalidPriority
	}
	return nil
}

// Apply copies the provided fields onto t and bumps UpdatedAt.
func (in UpdateInput) Apply(t *Todo, now time.Time) {
	if in.Title != nil {
		t.Title = *in.Title
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Completed != nil {
		t.Completed = *in.Completed
	}
	if in.Priority != nil {
		t.Priority = *in.Priority
	}
	if in.ClearDueDate {
		t.DueDate = nil
	} else if in.DueDate != nil {
		d := *in.DueDate
		t.DueDate = &d
	}
	t.UpdatedAt = now
}

func validateTitle(title string) error {
	if title == "" {
		return ErrTitleRequired
	}
	if len([]rune(title)) > MaxTitleLength {
		return ErrTitleTooLong
	}
	return nil
}

// IsValidationError reports whether err is one of the input validation errors.
func IsValidationError(err error) bool {
	return errors.Is(err, ErrTitleRequired) ||
		errors.Is(err, ErrTitleTooLong) ||
		errors.Is(err, ErrDescriptionTooLong) ||
		errors.Is(err, ErrInvalidPriority)
}

// Overdue reports whether t is past its due date and not completed.
func (t Todo) Overdue(now time.Time) bool {
	if t.Completed || t.DueDate == nil {
		return false
	}
	return now.After(*t.DueDate)
}
