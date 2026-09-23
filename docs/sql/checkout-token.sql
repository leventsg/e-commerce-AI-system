-- 手工执行；本次实现不连接数据库，不回填历史数据。
ALTER TABLE checkouts
    ADD COLUMN checkout_token CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '服务端随机购买意图令牌',
    ADD COLUMN checkout_intent_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '领取请求幂等ID',
    ADD COLUMN checkout_request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '首次提交参数摘要',
    ADD COLUMN checkout_request JSON NULL COMMENT '规范化结算请求，供核对使用',
    ADD COLUMN prepare_state TINYINT NOT NULL DEFAULT 0 COMMENT '0待提交 1处理中 2成功 3失败 4待核对',
    ADD COLUMN token_expires_at BIGINT NOT NULL DEFAULT 0 COMMENT '令牌首次消费截止时间秒',
    ADD COLUMN prepare_started_at BIGINT NOT NULL DEFAULT 0 COMMENT '执行开始时间秒',
    ADD COLUMN prepare_result JSON NULL COMMENT '原始结算响应，用于重放',
    ADD UNIQUE KEY uk_checkout_token (checkout_token),
    ADD UNIQUE KEY uk_checkout_intent (user_id, checkout_intent_id),
    ADD INDEX idx_prepare_state_started (prepare_state, prepare_started_at);
