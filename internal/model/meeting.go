// Package model contains domain models for the application.
package model

import (
	"fmt"
	"time"
)

// Meeting represents a transcription meeting/session.
// It stores metadata about the meeting, while the actual transcription
// content is stored separately in the database.
type Meeting struct {
	ID          int    `db:"id" json:"id"`
	UserID      int    `db:"user_id" json:"userId"`
	Title       string `db:"title" json:"title"`
	SessionUUID string `db:"session_uuid" json:"session_uuid"`
	StartTime   int64  `db:"start_time" json:"startTime,omitempty"`
	EndTime     int64  `db:"end_time" json:"endTime,omitempty"`
	Duration    int    `db:"duration_seconds" json:"duration"` // Duration in seconds
	TotalWords  int    `db:"total_words" json:"total_words"`
	Tokens      int    `db:"tokens" json:"tokens"`
	FilePath    string `db:"file_path" json:"filePath,omitempty"`
	Language    string `db:"language" json:"language,omitempty"`
	Status      string `db:"status" json:"status"`
	CreatedAt   int64  `db:"created_at" json:"createdAt"`
	UpdatedAt   int64  `db:"updated_at" json:"updatedAt"`
	DeletedAt   int64  `db:"deleted_at" json:"deletedAt,omitempty"`
}

// MeetingStatus represents the status of a meeting.
type MeetingStatus string

const (
	// StatusActive indicates an active/ongoing meeting
	StatusActive MeetingStatus = "active"

	// StatusCompleted indicates a completed meeting
	StatusCompleted MeetingStatus = "completed"

	// StatusFailed indicates a failed meeting
	StatusFailed MeetingStatus = "failed"
)

// IsValid checks if the meeting status is valid.
//
// Returns:
//   - bool: True if status is valid
func (s MeetingStatus) IsValid() bool {
	switch s {
	case StatusActive, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

// String returns the string representation of the status.
//
// Returns:
//   - string: Status as string
func (s MeetingStatus) String() string {
	return string(s)
}

// NewMeeting creates a new meeting with default values.
//
// Parameters:
//   - userID: ID of the user creating the meeting
//   - title: Meeting title
//
// Returns:
//   - *Meeting: New meeting instance
func NewMeeting(userID int, title string) *Meeting {
	now := time.Now().Unix()
	return &Meeting{
		UserID:      userID,
		Title:       title,
		SessionUUID: "",
		StartTime:   now,
		EndTime:     0,
		Duration:    0,
		TotalWords:  0,
		Tokens:      0,
		FilePath:    "",
		Language:    "en",
		Status:      string(StatusActive),
		CreatedAt:   now,
		UpdatedAt:   now,
		DeletedAt:   0,
	}
}

// GetStatus returns the meeting status as a MeetingStatus type.
//
// Returns:
//   - MeetingStatus: Meeting status
func (m *Meeting) GetStatus() MeetingStatus {
	return MeetingStatus(m.Status)
}

// IsActive checks if the meeting is active.
//
// Returns:
//   - bool: True if meeting is active
func (m *Meeting) IsActive() bool {
	return m.Status == string(StatusActive)
}

// IsCompleted checks if the meeting is completed.
//
// Returns:
//   - bool: True if meeting is completed
func (m *Meeting) IsCompleted() bool {
	return m.Status == string(StatusCompleted)
}

// IsFailed checks if the meeting failed.
//
// Returns:
//   - bool: True if meeting failed
func (m *Meeting) IsFailed() bool {
	return m.Status == string(StatusFailed)
}

// SetStatus updates the meeting status.
//
// Parameters:
//   - status: New meeting status
func (m *Meeting) SetStatus(status MeetingStatus) {
	m.Status = string(status)
	m.UpdatedAt = time.Now().Unix()
}

// Complete marks the meeting as completed and sets the duration.
//
// Parameters:
//   - duration: Meeting duration in seconds
func (m *Meeting) Complete(duration int) {
	m.Status = string(StatusCompleted)
	m.Duration = duration
	m.UpdatedAt = time.Now().Unix()
}

// Fail marks the meeting as failed.
func (m *Meeting) Fail() {
	m.Status = string(StatusFailed)
	m.UpdatedAt = time.Now().Unix()
}

// UpdateTitle updates the meeting title.
//
// Parameters:
//   - title: New meeting title
func (m *Meeting) UpdateTitle(title string) {
	m.Title = title
	m.UpdatedAt = time.Now().Unix()
}

// UpdateDuration updates the meeting duration.
//
// Parameters:
//   - duration: New duration in seconds
func (m *Meeting) UpdateDuration(duration int) {
	m.Duration = duration
	m.UpdatedAt = time.Now().Unix()
}

// SetLanguage sets the meeting language.
//
// Parameters:
//   - language: Language code (e.g., "en", "zh", "es")
func (m *Meeting) SetLanguage(language string) {
	m.Language = language
	m.UpdatedAt = time.Now().Unix()
}

// GetDurationMinutes returns the meeting duration in minutes.
//
// Returns:
//   - int: Duration in minutes
func (m *Meeting) GetDurationMinutes() int {
	return m.Duration / 60
}

// GetDurationFormatted returns the meeting duration in a human-readable format.
//
// Returns:
//   - string: Formatted duration (e.g., "1h 23m", "45m", "12s")
func (m *Meeting) GetDurationFormatted() string {
	if m.Duration < 60 {
		return formatDuration(0, 0, m.Duration)
	}

	minutes := m.Duration / 60
	seconds := m.Duration % 60

	if minutes < 60 {
		return formatDuration(0, minutes, seconds)
	}

	hours := minutes / 60
	minutes = minutes % 60

	return formatDuration(hours, minutes, seconds)
}

// formatDuration formats duration components into a string.
func formatDuration(hours, minutes, seconds int) string {
	if hours > 0 {
		if minutes > 0 {
			return formatTime(hours, "h") + " " + formatTime(minutes, "m")
		}
		return formatTime(hours, "h")
	}

	if minutes > 0 {
		if seconds > 0 {
			return formatTime(minutes, "m") + " " + formatTime(seconds, "s")
		}
		return formatTime(minutes, "m")
	}

	return formatTime(seconds, "s")
}

// formatTime formats a time value with its unit.
func formatTime(value int, unit string) string {
	return fmt.Sprintf("%d%s", value, unit)
}

// BelongsToUser checks if the meeting belongs to the specified user.
//
// Parameters:
//   - userID: User ID to check
//
// Returns:
//   - bool: True if meeting belongs to user
func (m *Meeting) BelongsToUser(userID int) bool {
	return m.UserID == userID
}

// Validate validates the meeting model.
//
// Returns:
//   - error: Validation error if any field is invalid
func (m *Meeting) Validate() error {
	if m.UserID <= 0 {
		return &ValidationError{Field: "user_id", Message: "User ID is required"}
	}

	if m.Title == "" {
		return &ValidationError{Field: "title", Message: "Title is required"}
	}

	if len(m.Title) > 255 {
		return &ValidationError{Field: "title", Message: "Title is too long (max 255 characters)"}
	}

	if !MeetingStatus(m.Status).IsValid() {
		return &ValidationError{Field: "status", Message: "Invalid meeting status"}
	}

	if m.Duration < 0 {
		return &ValidationError{Field: "duration", Message: "Duration cannot be negative"}
	}

	return nil
}

// MeetingWithConsumption 包含会议信息和计费统计
type MeetingWithConsumption struct {
	*Meeting
	ConsumedSeconds      int64 `json:"consumedSeconds"`      // 实际消费的计费秒数
	TranscriptionSeconds int64 `json:"transcriptionSeconds"` // 转录秒数
	TranslationSeconds   int64 `json:"translationSeconds"`   // 翻译秒数
}
