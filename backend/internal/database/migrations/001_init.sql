CREATE TABLE IF NOT EXISTS users (
  id VARCHAR(36) PRIMARY KEY,
  phone VARCHAR(20) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  nickname VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL
);

CREATE TABLE IF NOT EXISTS scenes (
  id VARCHAR(64) PRIMARY KEY,
  title VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  difficulty VARCHAR(16) NOT NULL,
  category VARCHAR(32) NOT NULL,
  objective TEXT NOT NULL,
  roles_json JSON NOT NULL,
  outline_json JSON NOT NULL,
  starter_prompt TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sessions (
  id VARCHAR(36) PRIMARY KEY,
  user_id VARCHAR(36) NOT NULL,
  scene_id VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  ws_token VARCHAR(64) NOT NULL UNIQUE,
  ws_expires_at DATETIME(3) NOT NULL,
  started_at DATETIME(3) NOT NULL,
  ended_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_sessions_user_time (user_id, started_at),
  CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_sessions_scene FOREIGN KEY (scene_id) REFERENCES scenes(id)
);

CREATE TABLE IF NOT EXISTS turns (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  session_id VARCHAR(36) NOT NULL,
  seq INT NOT NULL,
  user_text TEXT NOT NULL,
  assistant_text TEXT NOT NULL,
  ts_start BIGINT NOT NULL,
  ts_end BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_turn_session_seq (session_id, seq),
  CONSTRAINT fk_turns_session FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS evaluations (
  id VARCHAR(36) PRIMARY KEY,
  event_id VARCHAR(36) NOT NULL UNIQUE,
  session_id VARCHAR(36) NOT NULL UNIQUE,
  user_id VARCHAR(36) NOT NULL,
  status VARCHAR(16) NOT NULL,
  overall_score DECIMAL(4,1) NULL,
  result_json JSON NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  INDEX idx_evaluations_user (user_id),
  CONSTRAINT fk_evaluations_session FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS event_outbox (
  event_id VARCHAR(36) PRIMARY KEY,
  topic VARCHAR(128) NOT NULL,
  event_key VARCHAR(64) NOT NULL,
  payload_json JSON NOT NULL,
  published_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_outbox_pending (published_at, created_at)
);

INSERT INTO scenes (id, title, description, difficulty, category, objective, roles_json, outline_json, starter_prompt, sort_order)
VALUES
  ('restaurant-order', '餐厅点餐', '在英文餐厅完成点餐、确认菜品并结账。', 'A2', '生活旅行', '完成点餐并成功结账', JSON_ARRAY('顾客', '服务员'), JSON_ARRAY('入座', '点餐', '确认', '结账'), 'Welcome! Are you ready to order?', 10),
  ('job-interview', '英文面试', '练习自我介绍、工作经历和岗位动机。', 'B1', '职场', '清晰回答三类核心面试问题', JSON_ARRAY('候选人', '面试官'), JSON_ARRAY('自我介绍', '经历', '动机', '提问'), 'Tell me about yourself.', 20)
ON DUPLICATE KEY UPDATE title = VALUES(title);
