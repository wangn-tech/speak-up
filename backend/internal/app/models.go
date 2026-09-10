package app

import "time"

type User struct {
	ID           string    `db:"id" json:"user_id"`
	Phone        string    `db:"phone" json:"phone"`
	PasswordHash string    `db:"password_hash" json:"-"`
	Nickname     string    `db:"nickname" json:"nickname"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

type Scene struct {
	ID            string   `db:"id" json:"scene_id"`
	Title         string   `db:"title" json:"title"`
	Description   string   `db:"description" json:"description"`
	Difficulty    string   `db:"difficulty" json:"difficulty"`
	Category      string   `db:"category" json:"category"`
	Objective     string   `db:"objective" json:"objective"`
	RolesJSON     string   `db:"roles_json" json:"-"`
	OutlineJSON   string   `db:"outline_json" json:"-"`
	StarterPrompt string   `db:"starter_prompt" json:"starter_prompt"`
	Roles         []string `db:"-" json:"roles"`
	Outline       []string `db:"-" json:"outline_steps"`
}

type Session struct {
	ID           string     `db:"id" json:"session_id"`
	UserID       string     `db:"user_id" json:"-"`
	SceneID      string     `db:"scene_id" json:"scene_id"`
	SceneTitle   string     `db:"scene_title" json:"scene_title"`
	Status       string     `db:"status" json:"status"`
	WSToken      string     `db:"ws_token" json:"-"`
	WSExpiresAt  time.Time  `db:"ws_expires_at" json:"-"`
	StartedAt    time.Time  `db:"started_at" json:"started_at"`
	EndedAt      *time.Time `db:"ended_at" json:"ended_at,omitempty"`
	OverallScore *float64   `db:"overall_score" json:"overall_score,omitempty"`
	Turns        []Turn     `db:"-" json:"turns,omitempty"`
}

type Turn struct {
	Seq           uint32 `db:"seq" json:"seq"`
	UserText      string `db:"user_text" json:"user_text"`
	AssistantText string `db:"assistant_text" json:"assistant_text"`
	TSStart       int64  `db:"ts_start" json:"ts_start"`
	TSEnd         int64  `db:"ts_end" json:"ts_end"`
}

type Evaluation struct {
	ID           string         `db:"id" json:"evaluation_id"`
	EventID      string         `db:"event_id" json:"event_id"`
	SessionID    string         `db:"session_id" json:"session_id"`
	Status       string         `db:"status" json:"status"`
	OverallScore *float64       `db:"overall_score" json:"overall_score,omitempty"`
	ResultJSON   *string        `db:"result_json" json:"-"`
	Result       map[string]any `db:"-" json:"result,omitempty"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
}

type EvaluationTrigger struct {
	EventID       string `json:"event_id"`
	SessionID     string `json:"session_id"`
	UserID        string `json:"user_id"`
	Transcript    []Turn `json:"transcript"`
	SchemaVersion string `json:"schema_version"`
}

type EvaluationCompleted struct {
	EventID       string         `json:"event_id"`
	TaskID        string         `json:"task_id"`
	SessionID     string         `json:"session_id"`
	UserID        string         `json:"user_id"`
	Status        string         `json:"status"`
	Result        map[string]any `json:"result_json"`
	SchemaVersion string         `json:"schema_version"`
}
