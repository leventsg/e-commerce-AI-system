CREATE TABLE checkouts
(
    pre_order_id    VARCHAR(36)  NOT NULL COMMENT '预订单ID',
    user_id         INT UNSIGNED NOT NULL COMMENT '用户ID',
    coupon_id       VARCHAR(36) COMMENT '使用优惠券ID列表（支持多券）',
    original_amount BIGINT       NOT NULL COMMENT '原始金额（单位：分）',
    final_amount    BIGINT       NOT NULL COMMENT '实付金额（单位：分）',
    status          TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '状态：0-预占中 1-已确认 2-已取消 3-已过期',
    expire_time     BIGINT       NOT NULL COMMENT '过期时间戳（秒）',
    created_at      TIMESTAMP(3)          DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间（毫秒精度）',
    updated_at      TIMESTAMP(3)          DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    checkout_token CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '服务端随机购买意图令牌',
    checkout_intent_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '领取请求幂等ID',
    checkout_request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL COMMENT '首次提交参数摘要',
    checkout_request JSON NULL COMMENT '规范化结算请求，供核对使用',
    prepare_state TINYINT NOT NULL DEFAULT 0 COMMENT '0待提交 1处理中 2成功 3失败 4待核对',
    token_expires_at BIGINT NOT NULL DEFAULT 0 COMMENT '令牌首次消费截止时间秒',
    prepare_started_at BIGINT NOT NULL DEFAULT 0 COMMENT '执行开始时间秒',
    prepare_result JSON NULL COMMENT '原始结算响应，用于重放',
    PRIMARY KEY (pre_order_id),
    UNIQUE KEY uk_checkout_token (checkout_token),
    UNIQUE KEY uk_checkout_intent (user_id, checkout_intent_id),
    INDEX idx_prepare_state_started (prepare_state, prepare_started_at),
    INDEX idx_user_status (user_id, status),
    INDEX idx_expire (expire_time)
) COMMENT ='预订单主表';

CREATE TABLE checkout_items
(
    pre_order_id VARCHAR(64)  NOT NULL COMMENT '预订单ID',
    product_id   INT UNSIGNED NOT NULL COMMENT '商品ID',
    quantity     INT UNSIGNED NOT NULL COMMENT '数量',
    price        BIGINT       NOT NULL COMMENT '当时单价（分）',
    snapshot     JSON         NOT NULL COMMENT '商品快照（名称、规格等）',
    created_at   TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (pre_order_id),
    INDEX idx_product (product_id),
    INDEX idx_preorder (pre_order_id)
) COMMENT ='预订单商品明细';