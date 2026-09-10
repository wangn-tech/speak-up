package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("not found")

type OutboxEvent struct {
	EventID string `db:"event_id"`
	Topic   string `db:"topic"`
	Key     string `db:"event_key"`
	Payload []byte `db:"payload_json"`
}

type Store interface {
	CreateUser(context.Context, User) error
	UserByPhone(context.Context, string) (User, error)
	ListScenes(context.Context) ([]Scene, error)
	SceneByID(context.Context, string) (Scene, error)
	CreateSession(context.Context, Session) error
	SessionByID(context.Context, string, string) (Session, error)
	SessionByWSToken(context.Context, string, string) (Session, error)
	ListSessions(context.Context, string) ([]Session, error)
	SaveTurn(context.Context, string, Turn) error
	EndSession(context.Context, string, string) (string, error)
	EvaluationBySession(context.Context, string, string) (Evaluation, error)
	CompleteEvaluation(context.Context, EvaluationCompleted) error
	PendingOutbox(context.Context, int) ([]OutboxEvent, error)
	MarkOutboxPublished(context.Context, string) error
}

type MySQLStore struct{ db *sqlx.DB }

func NewMySQLStore(db *sqlx.DB) *MySQLStore { return &MySQLStore{db: db} }

func (s *MySQLStore) CreateUser(ctx context.Context, user User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, phone, password_hash, nickname, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, user.ID, user.Phone, user.PasswordHash, user.Nickname, user.CreatedAt, user.CreatedAt)
	return err
}

func (s *MySQLStore) UserByPhone(ctx context.Context, phone string) (User, error) {
	var user User
	err := s.db.GetContext(ctx, &user, `SELECT id, phone, password_hash, nickname, created_at FROM users WHERE phone = ?`, phone)
	return user, normalizeNotFound(err)
}

func (s *MySQLStore) ListScenes(ctx context.Context) ([]Scene, error) {
	var scenes []Scene
	err := s.db.SelectContext(ctx, &scenes, `SELECT id, title, description, difficulty, category, objective, roles_json, outline_json, starter_prompt FROM scenes ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	for i := range scenes {
		decodeScene(&scenes[i])
	}
	return scenes, nil
}

func (s *MySQLStore) SceneByID(ctx context.Context, id string) (Scene, error) {
	var scene Scene
	err := s.db.GetContext(ctx, &scene, `SELECT id, title, description, difficulty, category, objective, roles_json, outline_json, starter_prompt FROM scenes WHERE id = ?`, id)
	if err == nil {
		decodeScene(&scene)
	}
	return scene, normalizeNotFound(err)
}

func (s *MySQLStore) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, scene_id, status, ws_token, ws_expires_at, started_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.SceneID, session.Status, session.WSToken, session.WSExpiresAt, session.StartedAt, session.StartedAt)
	return err
}

func (s *MySQLStore) SessionByID(ctx context.Context, id, userID string) (Session, error) {
	var session Session
	err := s.db.GetContext(ctx, &session, sessionSelect+` WHERE s.id = ? AND s.user_id = ?`, id, userID)
	if err != nil {
		return session, normalizeNotFound(err)
	}
	turns, turnsErr := s.turns(ctx, id)
	if turnsErr != nil {
		return Session{}, turnsErr
	}
	session.Turns = turns
	return session, nil
}

func (s *MySQLStore) SessionByWSToken(ctx context.Context, id, token string) (Session, error) {
	var session Session
	err := s.db.GetContext(ctx, &session, sessionSelect+` WHERE s.id = ? AND s.ws_token = ? AND s.ws_expires_at > NOW(3)`, id, token)
	return session, normalizeNotFound(err)
}

func (s *MySQLStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	var sessions []Session
	err := s.db.SelectContext(ctx, &sessions, sessionSelect+` WHERE s.user_id = ? ORDER BY s.started_at DESC LIMIT 100`, userID)
	return sessions, err
}

func (s *MySQLStore) SaveTurn(ctx context.Context, sessionID string, turn Turn) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO turns (session_id, seq, user_text, assistant_text, ts_start, ts_end, created_at) VALUES (?, ?, ?, ?, ?, ?, NOW(3)) ON DUPLICATE KEY UPDATE user_text=VALUES(user_text), assistant_text=VALUES(assistant_text), ts_end=VALUES(ts_end)`, sessionID, turn.Seq, turn.UserText, turn.AssistantText, turn.TSStart, turn.TSEnd)
	return err
}

func (s *MySQLStore) EndSession(ctx context.Context, id, userID string) (string, error) {
	turns, err := s.turns(ctx, id)
	if err != nil {
		return "", err
	}
	if turns == nil {
		turns = []Turn{}
	}
	eventID := uuid.NewString()
	evaluationID := uuid.NewString()
	payload, err := json.Marshal(EvaluationTrigger{EventID: eventID, SessionID: id, UserID: userID, Transcript: turns, SchemaVersion: "1"})
	if err != nil {
		return "", fmt.Errorf("marshal evaluation trigger: %w", err)
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET status='finished', ended_at=NOW(3) WHERE id=? AND user_id=? AND status='active'`, id, userID)
	if err != nil {
		return "", err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return "", ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO evaluations (id, event_id, session_id, user_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'pending', NOW(3), NOW(3))`, evaluationID, eventID, id, userID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_outbox (event_id, topic, event_key, payload_json, created_at) VALUES (?, 'evaluation.trigger', ?, ?, NOW(3))`, eventID, id, payload); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return evaluationID, nil
}

func (s *MySQLStore) EvaluationBySession(ctx context.Context, sessionID, userID string) (Evaluation, error) {
	var evaluation Evaluation
	err := s.db.GetContext(ctx, &evaluation, `SELECT id, event_id, session_id, status, overall_score, result_json, created_at FROM evaluations WHERE session_id=? AND user_id=?`, sessionID, userID)
	if err != nil {
		return evaluation, normalizeNotFound(err)
	}
	if evaluation.ResultJSON != nil {
		_ = json.Unmarshal([]byte(*evaluation.ResultJSON), &evaluation.Result)
	}
	return evaluation, nil
}

func (s *MySQLStore) CompleteEvaluation(ctx context.Context, completed EvaluationCompleted) error {
	resultJSON, err := json.Marshal(completed.Result)
	if err != nil {
		return err
	}
	var score any
	if value, ok := completed.Result["overall_score"].(float64); ok {
		score = value
	}
	_, err = s.db.ExecContext(ctx, `UPDATE evaluations SET status=?, overall_score=?, result_json=?, updated_at=NOW(3) WHERE event_id=? AND session_id=? AND user_id=?`, completed.Status, score, resultJSON, completed.EventID, completed.SessionID, completed.UserID)
	return err
}

func (s *MySQLStore) PendingOutbox(ctx context.Context, limit int) ([]OutboxEvent, error) {
	var events []OutboxEvent
	err := s.db.SelectContext(ctx, &events, `SELECT event_id, topic, event_key, payload_json FROM event_outbox WHERE published_at IS NULL ORDER BY created_at LIMIT ?`, limit)
	return events, err
}

func (s *MySQLStore) MarkOutboxPublished(ctx context.Context, eventID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE event_outbox SET published_at=NOW(3) WHERE event_id=? AND published_at IS NULL`, eventID)
	return err
}

func (s *MySQLStore) turns(ctx context.Context, sessionID string) ([]Turn, error) {
	var turns []Turn
	err := s.db.SelectContext(ctx, &turns, `SELECT seq, user_text, assistant_text, ts_start, ts_end FROM turns WHERE session_id=? ORDER BY seq`, sessionID)
	return turns, err
}

const sessionSelect = `SELECT s.id, s.user_id, s.scene_id, sc.title AS scene_title, s.status, s.ws_token, s.ws_expires_at, s.started_at, s.ended_at, e.overall_score FROM sessions s JOIN scenes sc ON sc.id=s.scene_id LEFT JOIN evaluations e ON e.session_id=s.id`

func normalizeNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func decodeScene(scene *Scene) {
	_ = json.Unmarshal([]byte(scene.RolesJSON), &scene.Roles)
	_ = json.Unmarshal([]byte(scene.OutlineJSON), &scene.Outline)
}

func NewSession(userID, sceneID string, ttl time.Duration) Session {
	now := time.Now().UTC()
	return Session{ID: uuid.NewString(), UserID: userID, SceneID: sceneID, Status: "active", WSToken: uuid.NewString(), WSExpiresAt: now.Add(ttl), StartedAt: now}
}
