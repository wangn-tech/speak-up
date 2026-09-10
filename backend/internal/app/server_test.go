package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	conversationv1 "github.com/wangn-tech/speak-up/gen/english_tutor/conversation/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type memoryStore struct {
	mu          sync.Mutex
	users       map[string]User
	scenes      map[string]Scene
	sessions    map[string]Session
	turnsByID   map[string][]Turn
	evaluations map[string]Evaluation
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:    map[string]User{},
		scenes:   map[string]Scene{"restaurant-order": {ID: "restaurant-order", Title: "餐厅点餐", Objective: "完成点餐"}},
		sessions: map[string]Session{}, turnsByID: map[string][]Turn{}, evaluations: map[string]Evaluation{},
	}
}

func (m *memoryStore) CreateUser(_ context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Phone] = user
	return nil
}
func (m *memoryStore) UserByPhone(_ context.Context, phone string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[phone]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}
func (m *memoryStore) ListScenes(context.Context) ([]Scene, error) {
	return []Scene{m.scenes["restaurant-order"]}, nil
}
func (m *memoryStore) SceneByID(_ context.Context, id string) (Scene, error) {
	scene, ok := m.scenes[id]
	if !ok {
		return Scene{}, ErrNotFound
	}
	return scene, nil
}
func (m *memoryStore) CreateSession(_ context.Context, session Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session.SceneTitle = m.scenes[session.SceneID].Title
	m.sessions[session.ID] = session
	return nil
}
func (m *memoryStore) SessionByID(_ context.Context, id, userID string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.UserID != userID {
		return Session{}, ErrNotFound
	}
	session.Turns = append([]Turn(nil), m.turnsByID[id]...)
	return session, nil
}
func (m *memoryStore) SessionByWSToken(_ context.Context, id, token string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.WSToken != token || time.Now().After(session.WSExpiresAt) {
		return Session{}, ErrNotFound
	}
	return session, nil
}
func (m *memoryStore) ListSessions(_ context.Context, userID string) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Session{}
	for _, session := range m.sessions {
		if session.UserID == userID {
			result = append(result, session)
		}
	}
	return result, nil
}
func (m *memoryStore) SaveTurn(_ context.Context, sessionID string, turn Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turnsByID[sessionID] = append(m.turnsByID[sessionID], turn)
	return nil
}
func (m *memoryStore) EndSession(_ context.Context, id, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok || session.UserID != userID {
		return "", ErrNotFound
	}
	session.Status = "finished"
	m.sessions[id] = session
	return "evaluation-1", nil
}
func (m *memoryStore) EvaluationBySession(_ context.Context, sessionID, _ string) (Evaluation, error) {
	value, ok := m.evaluations[sessionID]
	if !ok {
		return Evaluation{}, ErrNotFound
	}
	return value, nil
}
func (m *memoryStore) CompleteEvaluation(_ context.Context, value EvaluationCompleted) error {
	m.evaluations[value.SessionID] = Evaluation{EventID: value.EventID, SessionID: value.SessionID, Status: value.Status, Result: value.Result}
	return nil
}
func (m *memoryStore) PendingOutbox(context.Context, int) ([]OutboxEvent, error) { return nil, nil }
func (m *memoryStore) MarkOutboxPublished(context.Context, string) error         { return nil }

func TestAuthSceneAndSessionOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMemoryStore()
	server := NewServer(testConfig(), store, nil).Router()

	first := registerUser(t, server, "13800000000")
	response := perform(t, server, http.MethodGet, "/api/v1/scenes", nil, first)
	if response.Code != http.StatusOK {
		t.Fatalf("list scenes status = %d", response.Code)
	}

	created := perform(t, server, http.MethodPost, "/api/v1/sessions", map[string]string{"scene_id": "restaurant-order"}, first)
	var envelope struct {
		Data SessionConnection `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.SessionID == "" {
		t.Fatal("missing session id")
	}

	second := registerUser(t, server, "13900000000")
	denied := perform(t, server, http.MethodGet, "/api/v1/sessions/"+envelope.Data.SessionID, nil, second)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-user status = %d", denied.Code)
	}
}

func TestAuthenticationFailureAndRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := NewServer(testConfig(), newMemoryStore(), nil).Router()

	unauthorized := perform(t, server, http.MethodGet, "/api/v1/scenes", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d", unauthorized.Code)
	}

	registered := perform(t, server, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"phone": "13700000000", "password": "speakup123", "nickname": "Ada",
	}, "")
	var tokens struct {
		Data struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	if tokens.Data.RefreshToken == "" {
		t.Fatal("missing refresh token")
	}

	badLogin := perform(t, server, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"phone": "13700000000", "password": "wrong-password",
	}, "")
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", badLogin.Code)
	}

	refreshed := perform(t, server, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": tokens.Data.RefreshToken,
	}, "")
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", refreshed.Code, refreshed.Body.String())
	}
}

type SessionConnection struct {
	SessionID string `json:"session_id"`
}

func TestTextWebSocketRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMemoryStore()
	grpcConn, closeGRPC := fakeGRPC(t)
	defer closeGRPC()
	session := NewSession("user-1", "restaurant-order", time.Minute)
	if err := store.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(NewServer(testConfig(), store, grpcConn).Router())
	defer httpServer.Close()

	url := "ws" + httpServer.URL[len("http"):] + "/ws/conversation?session_id=" + session.ID + "&token=" + session.WSToken
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close websocket: %v", closeErr)
		}
	}()
	if err = conn.WriteJSON(map[string]any{"type": "heartbeat", "payload": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	var heartbeat struct {
		Type string `json:"type"`
	}
	if err = conn.ReadJSON(&heartbeat); err != nil {
		t.Fatal(err)
	}
	if heartbeat.Type != "pong" {
		t.Fatalf("heartbeat response = %s", heartbeat.Type)
	}
	if err = conn.WriteJSON(map[string]any{"type": "text", "payload": map[string]any{"turn_seq": 1, "text": "Hello"}}); err != nil {
		t.Fatal(err)
	}

	seenReply, seenEnd := false, false
	for range 2 {
		var frame struct {
			Type string `json:"type"`
		}
		if err = conn.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		seenReply = seenReply || frame.Type == "reply_delta"
		seenEnd = seenEnd || frame.Type == "turn_end"
	}
	if !seenReply || !seenEnd {
		t.Fatalf("reply=%v end=%v", seenReply, seenEnd)
	}
	time.Sleep(20 * time.Millisecond)
	store.mu.Lock()
	savedTurns := len(store.turnsByID[session.ID])
	store.mu.Unlock()
	if savedTurns != 1 {
		t.Fatalf("saved turns = %d", savedTurns)
	}
}

type fakeConversation struct {
	conversationv1.UnimplementedConversationServiceServer
}

func (fakeConversation) StreamChat(stream grpc.BidiStreamingServer[conversationv1.ClientEvent, conversationv1.ServerEvent]) error {
	event, err := stream.Recv()
	if err != nil {
		return err
	}
	if event.GetTextInput().GetText() != "Hello" {
		return errors.New("unexpected text")
	}
	if err = stream.Send(&conversationv1.ServerEvent{SessionId: event.SessionId, Type: conversationv1.EventType_EVENT_TYPE_REPLY_DELTA, PayloadJson: `{"delta":"Hi!","turn_seq":1}`}); err != nil {
		return err
	}
	return stream.Send(&conversationv1.ServerEvent{SessionId: event.SessionId, Type: conversationv1.EventType_EVENT_TYPE_TURN_END, PayloadJson: `{"turn_seq":1}`})
}

func fakeGRPC(t *testing.T) (*grpc.ClientConn, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	conversationv1.RegisterConversationServiceServer(server, fakeConversation{})
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return conn, func() { _ = conn.Close(); server.Stop(); _ = listener.Close() }
}

func registerUser(t *testing.T, handler http.Handler, phone string) string {
	t.Helper()
	response := perform(t, handler, http.MethodPost, "/api/v1/auth/register", map[string]string{"phone": phone, "password": "speakup123", "nickname": "Ada"}, "")
	if response.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.AccessToken
}

func perform(t *testing.T, handler http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &payload)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func testConfig() Config { config := ConfigFromEnv(); config.JWTSecret = "test-secret"; return config }
