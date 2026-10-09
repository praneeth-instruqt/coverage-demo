package todo

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func TestPriorityValid(t *testing.T) {
	for _, p := range []Priority{PriorityLow, PriorityMedium, PriorityHigh} {
		if !p.Valid() {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range []Priority{"", "urgent", "HIGH"} {
		if p.Valid() {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func TestCreateInputNormalize(t *testing.T) {
	in := CreateInput{Title: "  buy milk  ", Description: "\n2 litres\t"}
	in.Normalize()
	if in.Title != "buy milk" || in.Description != "2 litres" {
		t.Errorf("not trimmed: %+v", in)
	}
	if in.Priority != PriorityMedium {
		t.Errorf("default priority = %q, want medium", in.Priority)
	}

	in = CreateInput{Title: "x", Priority: PriorityHigh}
	in.Normalize()
	if in.Priority != PriorityHigh {
		t.Errorf("explicit priority overwritten: %q", in.Priority)
	}
}

func TestCreateInputValidate(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
		want error
	}{
		{"valid", CreateInput{Title: "ok", Priority: PriorityLow}, nil},
		{"empty title", CreateInput{Title: "", Priority: PriorityLow}, ErrTitleRequired},
		{"title too long", CreateInput{Title: strings.Repeat("a", MaxTitleLength+1), Priority: PriorityLow}, ErrTitleTooLong},
		{"title at limit in runes", CreateInput{Title: strings.Repeat("é", MaxTitleLength), Priority: PriorityLow}, nil},
		{"description too long", CreateInput{Title: "ok", Description: strings.Repeat("a", MaxDescriptionLength+1), Priority: PriorityLow}, ErrDescriptionTooLong},
		{"bad priority", CreateInput{Title: "ok", Priority: "urgent"}, ErrInvalidPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); !errors.Is(err, tt.want) {
				t.Errorf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestUpdateInputNormalize(t *testing.T) {
	in := UpdateInput{Title: ptr("  a "), Description: ptr(" b ")}
	in.Normalize()
	if *in.Title != "a" || *in.Description != "b" {
		t.Errorf("not trimmed: %q %q", *in.Title, *in.Description)
	}

	empty := UpdateInput{}
	empty.Normalize()
	if empty.Title != nil || empty.Description != nil {
		t.Error("nil fields should stay nil")
	}
}

func TestUpdateInputValidate(t *testing.T) {
	tests := []struct {
		name string
		in   UpdateInput
		want error
	}{
		{"empty update", UpdateInput{}, nil},
		{"valid all", UpdateInput{Title: ptr("t"), Description: ptr("d"), Priority: ptr(PriorityHigh)}, nil},
		{"blank title", UpdateInput{Title: ptr("")}, ErrTitleRequired},
		{"long title", UpdateInput{Title: ptr(strings.Repeat("a", MaxTitleLength+1))}, ErrTitleTooLong},
		{"long description", UpdateInput{Description: ptr(strings.Repeat("a", MaxDescriptionLength+1))}, ErrDescriptionTooLong},
		{"bad priority", UpdateInput{Priority: ptr(Priority("nope"))}, ErrInvalidPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); !errors.Is(err, tt.want) {
				t.Errorf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestUpdateInputApply(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	now := created.Add(time.Hour)

	t.Run("sets all fields", func(t *testing.T) {
		td := Todo{Title: "old", Priority: PriorityLow, CreatedAt: created, UpdatedAt: created}
		UpdateInput{
			Title: ptr("new"), Description: ptr("desc"), Completed: ptr(true),
			Priority: ptr(PriorityHigh), DueDate: &due,
		}.Apply(&td, now)

		if td.Title != "new" || td.Description != "desc" || !td.Completed || td.Priority != PriorityHigh {
			t.Errorf("fields not applied: %+v", td)
		}
		if td.DueDate == nil || !td.DueDate.Equal(due) {
			t.Errorf("due date = %v, want %v", td.DueDate, due)
		}
		if td.DueDate == &due {
			t.Error("due date should be copied, not aliased")
		}
		if !td.UpdatedAt.Equal(now) || !td.CreatedAt.Equal(created) {
			t.Errorf("timestamps wrong: created=%v updated=%v", td.CreatedAt, td.UpdatedAt)
		}
	})

	t.Run("leaves nil fields untouched", func(t *testing.T) {
		td := Todo{Title: "keep", Description: "keep", Completed: true, Priority: PriorityLow, DueDate: &due}
		UpdateInput{}.Apply(&td, now)
		if td.Title != "keep" || td.Description != "keep" || !td.Completed || td.Priority != PriorityLow || td.DueDate == nil {
			t.Errorf("fields changed: %+v", td)
		}
	})

	t.Run("clear due date wins over new due date", func(t *testing.T) {
		td := Todo{DueDate: &due}
		UpdateInput{ClearDueDate: true, DueDate: &due}.Apply(&td, now)
		if td.DueDate != nil {
			t.Errorf("due date = %v, want nil", td.DueDate)
		}
	})
}

func TestIsValidationError(t *testing.T) {
	for _, err := range []error{ErrTitleRequired, ErrTitleTooLong, ErrDescriptionTooLong, ErrInvalidPriority} {
		if !IsValidationError(err) {
			t.Errorf("%v should be a validation error", err)
		}
	}
	for _, err := range []error{nil, ErrNotFound, errors.New("other")} {
		if IsValidationError(err) {
			t.Errorf("%v should not be a validation error", err)
		}
	}
}
