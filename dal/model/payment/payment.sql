CREATE TABLE payments
(
    payment_id      VARCHAR(36)  NOT NULL COMMENT '支付单ID（UUID）',
    pre_order_id    VARCHAR(64)  NOT NULL COMMENT '预订单ID（外键）',
    order_id        VARCHAR(36)  DEFAULT NULL COMMENT '关联订单ID（支付成功后更新）',
    user_id         INT UNSIGNED NOT NULL COMMENT '用户ID',
    original_amount BIGINT       NOT NULL COMMENT '订单原价（单位：分）',
    paid_amount     BIGINT       DEFAULT NULL COMMENT '实付金额（分）',

    -- 支付信息
    payment_method  VARCHAR(20)  NOT NULL COMMENT '支付渠道（wx_pay/alipay）',
    transaction_id  VARCHAR(64)  DEFAULT NULL COMMENT '支付平台交易号',
    pay_url         Text         NOT NULL COMMENT '支付跳转链接',
    expire_time     BIGINT       NOT NULL COMMENT '支付链接过期时间戳（秒）',

    status          TINYINT      NOT NULL COMMENT '支付状态（0-未定义 1-待支付 2-已支付...）',

    created_at      TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间（毫秒精度）',
    updated_at      TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    paid_at         BIGINT       DEFAULT NULL COMMENT '支付成功时间戳（秒）',

    PRIMARY KEY (payment_id),
    INDEX idx_pre_order (pre_order_id),
    INDEX idx_order (order_id),
    INDEX idx_status_method (status, payment_method),
    INDEX idx_create_time (created_at)
) COMMENT ='支付表';

CREATE TABLE payment_outbox_messages
(
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键',
    message_id      VARCHAR(64)     NOT NULL COMMENT '消息唯一ID',
    event_type      VARCHAR(64)     NOT NULL COMMENT '事件类型',
    topic           VARCHAR(128)    NOT NULL COMMENT 'MQ topic',
    message_key     VARCHAR(128)             DEFAULT '' COMMENT 'MQ message key',
    payload         JSON            NOT NULL COMMENT '消息体',
    status          TINYINT         NOT NULL DEFAULT 0 COMMENT '0-pending 1-sending 2-sent 3-failed',
    retry_count     INT             NOT NULL DEFAULT 0 COMMENT '已重试次数',
    max_retry_count INT             NOT NULL DEFAULT 10 COMMENT '最大重试次数',
    next_retry_at   TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '下次可重试时间',
    locked_until    TIMESTAMP       NULL     DEFAULT NULL COMMENT '发送锁过期时间',
    last_error      VARCHAR(1024)            DEFAULT NULL COMMENT '最近一次错误',
    sent_at         TIMESTAMP       NULL     DEFAULT NULL COMMENT '发送成功时间',
    created_at      TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at      TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_message_id (message_id),
    UNIQUE KEY uk_event_key (event_type, message_key),
    KEY idx_status_retry (status, next_retry_at),
    KEY idx_locked_until (locked_until)
) COMMENT ='payment服务本地消息表';
