CREATE TABLE inventory
(
    product_id INT, -- 与商品服务共享同一ID
    total      INT NOT NULL,
    sold       INT NOT NULL,
    primary key (product_id)
);

CREATE TABLE `inventory_lock` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id` VARCHAR(64) NOT NULL COMMENT '业务单号，当前代码里实际可能是 pre_order_id',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_order_user` (`order_id`, `user_id`),
    KEY `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='库存真实扣减幂等锁表';

CREATE TABLE `return_lock` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id` VARCHAR(64) NOT NULL COMMENT '业务单号，当前代码里实际可能是 pre_order_id',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_order_user` (`order_id`, `user_id`),
    KEY `idx_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='库存归还幂等锁表';