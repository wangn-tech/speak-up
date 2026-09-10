package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	conversationv1 "github.com/wangn-tech/speak-up/gen/english_tutor/conversation/v1"
)

type wsControl struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type wsTurnPayload struct {
	TurnSeq uint32 `json:"turn_seq"`
	Text    string `json:"text"`
}

type turnCapture struct {
	userText string
	reply    string
	started  int64
}

func (s *Server) handleConversation(c *gin.Context) {
	if s.aiConn == nil {
		fail(c, http.StatusServiceUnavailable, "ERR_AI_UNAVAILABLE", "AI 服务未连接")
		return
	}
	sessionID, token := c.Query("session_id"), c.Query("token")
	session, err := s.store.SessionByWSToken(c, sessionID, token)
	if err != nil {
		fail(c, http.StatusUnauthorized, "ERR_AUTH", "WebSocket 凭证无效或已过期")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == s.config.AllowedOrigin
	}}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Debug("close websocket", "error", closeErr)
		}
	}()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	stream, err := conversationv1.NewConversationServiceClient(s.aiConn).StreamChat(ctx)
	if err != nil {
		_ = conn.WriteJSON(errorFrame("ERR_AI_UNAVAILABLE", "无法建立 AI 会话", true))
		return
	}

	var writeMu sync.Mutex
	writeJSON := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(value)
	}
	turns := map[uint32]*turnCapture{}
	var turnsMu sync.Mutex
	receiveDone := make(chan error, 1)
	go func() {
		for {
			event, recvErr := stream.Recv()
			if recvErr != nil {
				receiveDone <- recvErr
				return
			}
			frameType := serverEventType(event.GetType())
			var payload any
			if err := json.Unmarshal([]byte(event.GetPayloadJson()), &payload); err != nil {
				payload = map[string]any{"raw": event.GetPayloadJson()}
			}
			captureServerEvent(event, turns, &turnsMu)
			if event.GetType() == conversationv1.EventType_EVENT_TYPE_TURN_END {
				seq := payloadTurnSeq(event.GetPayloadJson())
				turnsMu.Lock()
				capture := turns[seq]
				delete(turns, seq)
				turnsMu.Unlock()
				if capture != nil {
					saveCtx, saveCancel := context.WithTimeout(context.Background(), 3*time.Second)
					_ = s.store.SaveTurn(saveCtx, session.ID, Turn{Seq: seq, UserText: capture.userText, AssistantText: capture.reply, TSStart: capture.started, TSEnd: time.Now().UnixMilli()})
					saveCancel()
				}
			}
			if err := writeJSON(gin.H{"type": frameType, "payload": payload}); err != nil {
				receiveDone <- err
				return
			}
		}
	}()

	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	var activeTurn uint32
	var chunkSeq uint32
	for {
		select {
		case recvErr := <-receiveDone:
			if !errors.Is(recvErr, io.EOF) && !errors.Is(recvErr, context.Canceled) {
				_ = writeJSON(errorFrame("ERR_AI_STREAM", "AI 流已中断", true))
			}
			return
		default:
		}
		messageType, body, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		if messageType == websocket.BinaryMessage {
			if activeTurn == 0 || len(body) == 0 {
				continue
			}
			chunkSeq++
			err = stream.Send(clientEvent(session, activeTurn, &conversationv1.ClientEvent_AudioChunk{AudioChunk: &conversationv1.AudioChunk{Data: body, ChunkSeq: chunkSeq}}))
			if err != nil {
				return
			}
			continue
		}
		var control wsControl
		if err = json.Unmarshal(body, &control); err != nil {
			_ = writeJSON(errorFrame("ERR_BAD_FRAME", "无法解析消息", false))
			continue
		}
		var payload wsTurnPayload
		_ = json.Unmarshal(control.Payload, &payload)
		switch control.Type {
		case "start":
			_ = writeJSON(gin.H{"type": "started", "payload": gin.H{"session_id": session.ID}})
		case "heartbeat":
			_ = writeJSON(gin.H{"type": "pong", "payload": gin.H{"ts_ms": time.Now().UnixMilli()}})
		case "text":
			if payload.TurnSeq == 0 || payload.Text == "" {
				_ = writeJSON(errorFrame("ERR_BAD_FRAME", "turn_seq 和 text 必填", false))
				continue
			}
			turnsMu.Lock()
			turns[payload.TurnSeq] = &turnCapture{userText: payload.Text, started: time.Now().UnixMilli()}
			turnsMu.Unlock()
			err = stream.Send(clientEvent(session, payload.TurnSeq, &conversationv1.ClientEvent_TextInput{TextInput: &conversationv1.TextInput{Text: payload.Text}}))
		case "audio_start":
			if payload.TurnSeq == 0 {
				_ = writeJSON(errorFrame("ERR_BAD_FRAME", "turn_seq 必填", false))
				continue
			}
			activeTurn, chunkSeq = payload.TurnSeq, 0
			turnsMu.Lock()
			turns[activeTurn] = &turnCapture{started: time.Now().UnixMilli()}
			turnsMu.Unlock()
			err = stream.Send(clientEvent(session, activeTurn, &conversationv1.ClientEvent_StartTurn{StartTurn: &conversationv1.StartTurn{AudioFormat: "pcm_s16le", SampleRate: 16000, Channels: 1}}))
		case "audio_end":
			if activeTurn != 0 {
				err = stream.Send(clientEvent(session, activeTurn, &conversationv1.ClientEvent_EndTurn{EndTurn: &conversationv1.EndTurn{}}))
				activeTurn = 0
			}
		case "end":
			_ = stream.CloseSend()
			return
		case "interrupt":
			_ = writeJSON(errorFrame("ERR_INTERRUPT_UNSUPPORTED", "MVP 暂不支持打断", false))
		default:
			_ = writeJSON(errorFrame("ERR_BAD_FRAME", "未知消息类型", false))
		}
		if err != nil {
			_ = writeJSON(errorFrame("ERR_AI_STREAM", "发送 AI 事件失败", true))
			return
		}
	}
}

func clientEvent(session Session, turnSeq uint32, payload any) *conversationv1.ClientEvent {
	event := &conversationv1.ClientEvent{SessionId: session.ID, UserId: session.UserID, SceneId: session.SceneID, TurnSeq: turnSeq, TsMs: time.Now().UnixMilli()}
	switch value := payload.(type) {
	case *conversationv1.ClientEvent_StartTurn:
		event.Payload = value
	case *conversationv1.ClientEvent_AudioChunk:
		event.Payload = value
	case *conversationv1.ClientEvent_EndTurn:
		event.Payload = value
	case *conversationv1.ClientEvent_TextInput:
		event.Payload = value
	}
	return event
}

func serverEventType(eventType conversationv1.EventType) string {
	switch eventType {
	case conversationv1.EventType_EVENT_TYPE_ASR_PARTIAL:
		return "asr_partial"
	case conversationv1.EventType_EVENT_TYPE_ASR_FINAL:
		return "asr_final"
	case conversationv1.EventType_EVENT_TYPE_REPLY_DELTA:
		return "reply_delta"
	case conversationv1.EventType_EVENT_TYPE_TOOL_CALL, conversationv1.EventType_EVENT_TYPE_TOOL_RESULT:
		return "tool_event"
	case conversationv1.EventType_EVENT_TYPE_TTS_START:
		return "tts_start"
	case conversationv1.EventType_EVENT_TYPE_TTS_CHUNK:
		return "tts_chunk"
	case conversationv1.EventType_EVENT_TYPE_TURN_END:
		return "turn_end"
	default:
		return "error"
	}
}

func captureServerEvent(event *conversationv1.ServerEvent, turns map[uint32]*turnCapture, mu *sync.Mutex) {
	var payload map[string]any
	if json.Unmarshal([]byte(event.GetPayloadJson()), &payload) != nil {
		return
	}
	seq := payloadTurnSeq(event.GetPayloadJson())
	mu.Lock()
	defer mu.Unlock()
	capture := turns[seq]
	if capture == nil {
		capture = &turnCapture{started: time.Now().UnixMilli()}
		turns[seq] = capture
	}
	switch event.GetType() {
	case conversationv1.EventType_EVENT_TYPE_ASR_FINAL:
		if text, ok := payload["text"].(string); ok {
			capture.userText = text
		}
	case conversationv1.EventType_EVENT_TYPE_REPLY_DELTA:
		if delta, ok := payload["delta"].(string); ok {
			capture.reply += delta
		}
	}
}

func payloadTurnSeq(raw string) uint32 {
	var payload struct {
		TurnSeq uint32 `json:"turn_seq"`
	}
	_ = json.Unmarshal([]byte(raw), &payload)
	return payload.TurnSeq
}

func errorFrame(code, message string, retriable bool) gin.H {
	return gin.H{"type": "error", "payload": gin.H{"code": code, "message": message, "retriable": retriable}}
}
