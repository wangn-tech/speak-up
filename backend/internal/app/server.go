package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
)

const userIDKey = "user_id"

type Server struct {
	config Config
	store  Store
	aiConn *grpc.ClientConn
}

func NewServer(config Config, store Store, aiConn *grpc.ClientConn) *Server {
	return &Server{config: config, store: store, aiConn: aiConn}
}

func (s *Server) Router() http.Handler {
	router := gin.New()
	router.Use(gin.Recovery(), s.cors())
	router.GET("/healthz", func(c *gin.Context) { ok(c, gin.H{"status": "ok"}) })
	router.GET("/ws/conversation", s.handleConversation)

	api := router.Group("/api/v1")
	api.POST("/auth/register", s.register)
	api.POST("/auth/login", s.login)
	api.POST("/auth/refresh", s.refresh)

	authorized := api.Group("")
	authorized.Use(s.authenticate())
	authorized.GET("/scenes", s.listScenes)
	authorized.GET("/scenes/:id", s.getScene)
	authorized.POST("/sessions", s.createSession)
	authorized.GET("/sessions", s.listSessions)
	authorized.GET("/sessions/:id", s.getSession)
	authorized.POST("/sessions/:id/end", s.endSession)
	authorized.GET("/evaluations", s.getEvaluation)
	return router
}

type authRequest struct {
	Phone    string `json:"phone" binding:"required,min=6,max=20"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Nickname string `json:"nickname" binding:"omitempty,max=64"`
}

func (s *Server) register(c *gin.Context) {
	var input authRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "ERR_INVALID_ARGUMENT", "手机号和至少 8 位密码必填")
		return
	}
	if _, err := s.store.UserByPhone(c, input.Phone); err == nil {
		fail(c, http.StatusConflict, "ERR_USER_EXISTS", "手机号已注册")
		return
	} else if !errors.Is(err, ErrNotFound) {
		internal(c, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(c, err)
		return
	}
	if input.Nickname == "" {
		input.Nickname = "SpeakUp Learner"
	}
	user := User{ID: uuid.NewString(), Phone: input.Phone, PasswordHash: string(hash), Nickname: input.Nickname, CreatedAt: time.Now().UTC()}
	if err = s.store.CreateUser(c, user); err != nil {
		internal(c, err)
		return
	}
	s.respondWithTokens(c, user)
}

func (s *Server) login(c *gin.Context) {
	var input authRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "ERR_INVALID_ARGUMENT", "手机号和密码必填")
		return
	}
	user, err := s.store.UserByPhone(c, input.Phone)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)) != nil {
		fail(c, http.StatusUnauthorized, "ERR_AUTH", "手机号或密码错误")
		return
	}
	s.respondWithTokens(c, user)
}

func (s *Server) refresh(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "ERR_INVALID_ARGUMENT", "refresh_token 必填")
		return
	}
	claims, err := s.parseToken(input.RefreshToken, "refresh")
	if err != nil {
		fail(c, http.StatusUnauthorized, "ERR_AUTH", "refresh_token 无效或已过期")
		return
	}
	access, err := s.signToken(claims.Subject, "access", s.config.AccessTTL)
	if err != nil {
		internal(c, err)
		return
	}
	refresh, err := s.signToken(claims.Subject, "refresh", s.config.RefreshTTL)
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"access_token": access, "refresh_token": refresh})
}

func (s *Server) respondWithTokens(c *gin.Context, user User) {
	access, err := s.signToken(user.ID, "access", s.config.AccessTTL)
	if err != nil {
		internal(c, err)
		return
	}
	refresh, err := s.signToken(user.ID, "refresh", s.config.RefreshTTL)
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"access_token": access, "refresh_token": refresh, "user": user})
}

func (s *Server) listScenes(c *gin.Context) {
	scenes, err := s.store.ListScenes(c)
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"page": 1, "page_size": len(scenes), "total": len(scenes), "list": scenes})
}

func (s *Server) getScene(c *gin.Context) {
	scene, err := s.store.SceneByID(c, c.Param("id"))
	if errors.Is(err, ErrNotFound) {
		fail(c, http.StatusNotFound, "ERR_SCENE_NOT_FOUND", "场景不存在")
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, scene)
}

func (s *Server) createSession(c *gin.Context) {
	var input struct {
		SceneID string `json:"scene_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, http.StatusBadRequest, "ERR_INVALID_ARGUMENT", "scene_id 必填")
		return
	}
	if _, err := s.store.SceneByID(c, input.SceneID); err != nil {
		fail(c, http.StatusBadRequest, "ERR_SCENE_INVALID", "场景不可用")
		return
	}
	session := NewSession(currentUser(c), input.SceneID, s.config.WSTTL)
	if err := s.store.CreateSession(c, session); err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"session_id": session.ID, "ws_token": session.WSToken, "ws_url": s.config.PublicWSURL, "expires_in": int(s.config.WSTTL.Seconds())})
}

func (s *Server) listSessions(c *gin.Context) {
	sessions, err := s.store.ListSessions(c, currentUser(c))
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"page": 1, "page_size": len(sessions), "total": len(sessions), "list": sessions})
}

func (s *Server) getSession(c *gin.Context) {
	session, err := s.store.SessionByID(c, c.Param("id"), currentUser(c))
	if errors.Is(err, ErrNotFound) {
		fail(c, http.StatusNotFound, "ERR_SESSION_NOT_FOUND", "会话不存在")
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, session)
}

func (s *Server) endSession(c *gin.Context) {
	evaluationID, err := s.store.EndSession(c, c.Param("id"), currentUser(c))
	if errors.Is(err, ErrNotFound) {
		fail(c, http.StatusNotFound, "ERR_SESSION_NOT_FOUND", "进行中的会话不存在")
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, gin.H{"status": "finished", "evaluation_id": evaluationID})
}

func (s *Server) getEvaluation(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		fail(c, http.StatusBadRequest, "ERR_INVALID_ARGUMENT", "session_id 必填")
		return
	}
	evaluation, err := s.store.EvaluationBySession(c, sessionID, currentUser(c))
	if errors.Is(err, ErrNotFound) {
		ok(c, []Evaluation{})
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	ok(c, []Evaluation{evaluation})
}

func (s *Server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(c, http.StatusUnauthorized, "ERR_AUTH", "缺少访问令牌")
			c.Abort()
			return
		}
		claims, err := s.parseToken(strings.TrimPrefix(header, "Bearer "), "access")
		if err != nil {
			fail(c, http.StatusUnauthorized, "ERR_AUTH", "访问令牌无效或已过期")
			c.Abort()
			return
		}
		c.Set(userIDKey, claims.Subject)
		c.Next()
	}
}

func (s *Server) signToken(userID, kind string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{"sub": userID, "kind": kind, "iat": now.Unix(), "exp": now.Add(ttl).Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.config.JWTSecret))
}

func (s *Server) parseToken(raw, kind string) (*jwt.RegisteredClaims, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.config.JWTSecret), nil
	})
	if err != nil || !token.Valid || claims["kind"] != kind {
		return nil, errors.New("invalid token")
	}
	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return nil, errors.New("missing subject")
	}
	return &jwt.RegisteredClaims{Subject: subject}, nil
}

func (s *Server) cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == s.config.AllowedOrigin {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func currentUser(c *gin.Context) string { return c.GetString(userIDKey) }

func ok(c *gin.Context, data any) { c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data}) }

func fail(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "msg": message, "data": nil})
}

func internal(c *gin.Context, err error) {
	_ = c.Error(err)
	fail(c, http.StatusInternalServerError, "ERR_INTERNAL", "服务暂时不可用")
}
