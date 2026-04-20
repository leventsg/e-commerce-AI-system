/*
 Navicat Premium Dump SQL

 Source Server         : localhost
 Source Server Type    : MySQL
 Source Server Version : 80040 (8.0.40)
 Source Host           : localhost:3306
 Source Schema         : mall

 Target Server Type    : MySQL
 Target Server Version : 80040 (8.0.40)
 File Encoding         : 65001

 Date: 24/01/2025 14:50:23
*/


-- 创建一个单独的用户
CREATE USER IF NOT EXISTS 'leventsg'@'%' IDENTIFIED BY 'leventsg';
GRANT ALL PRIVILEGES ON *.* TO 'leventsg'@'%';

-- 创建数据库
create database if not exists mall character set utf8mb4;
SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- 使用数据库
USE mall;

-- ----------------------------
-- Table structure for shopping_cart
-- ----------------------------
DROP TABLE IF EXISTS `shopping_cart`;
CREATE TABLE `shopping_cart` (
    `id` int(11) NOT NULL AUTO_INCREMENT COMMENT '主键 自增',
    `created_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    `deleted_at` TIMESTAMP DEFAULT NULL COMMENT '删除时间',
    `is_deleted` tinyint(1) DEFAULT NULL COMMENT '记录是否删除',
    `user_id` int(11) DEFAULT NULL COMMENT '用户ID',
    `goods_id` int(11) DEFAULT NULL COMMENT '商品ID',
    `nums` int(11) DEFAULT NULL COMMENT '商品数量',
    `checked` tinyint(1) DEFAULT NULL COMMENT '商品是否选中',
    PRIMARY KEY (`id`),
    KEY `idx_shopping_cart_goods` (`goods_id`),
    KEY `idx_shopping_cart_user` (`user_id`)
);

-- ----------------------------
-- Table structure for carts
-- ----------------------------
DROP TABLE IF EXISTS `carts`;
CREATE TABLE `carts`  (
                          `id` int NOT NULL AUTO_INCREMENT COMMENT '主键 自增',
                          `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
                          `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
                          `deleted_at` timestamp NULL DEFAULT NULL COMMENT '删除时间',
                          `user_id` int NULL DEFAULT NULL COMMENT '用户ID',
                          `product_id` int NULL DEFAULT NULL COMMENT '商品ID',
                          `quantity` int NULL DEFAULT NULL COMMENT '商品数量',
                          `checked` tinyint(1) NULL DEFAULT NULL COMMENT '商品是否选中',
                          PRIMARY KEY (`id`) USING BTREE,
                          INDEX `idx_carts_user_id`(`user_id` ASC) USING BTREE,
                          INDEX `idx_carts_product_id`(`product_id` ASC) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci ROW_FORMAT = Dynamic;

-- ----------------------------
-- Table structure for categories
-- ----------------------------
DROP TABLE IF EXISTS `categories`;
CREATE TABLE `categories`  (
                               `id` int NOT NULL AUTO_INCREMENT COMMENT '主键，自增，分类id',
                               `NAME` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '分类名称',
                               `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL COMMENT '分类描述',
                               `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
                               `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
                               PRIMARY KEY (`id`) USING BTREE,
                               UNIQUE INDEX `idx_category_name`(`NAME` ASC) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci ROW_FORMAT = Dynamic;

-- ----------------------------
-- Table structure for product_categories
-- ----------------------------
DROP TABLE IF EXISTS `product_categories`;
CREATE TABLE `product_categories`  (
                                       `id` int NOT NULL AUTO_INCREMENT COMMENT '自增主键',
                                       `product_id` int NULL DEFAULT NULL COMMENT '商品id',
                                       `category_id` int NULL DEFAULT NULL COMMENT '分类id',
                                       PRIMARY KEY (`id`) USING BTREE,
                                       UNIQUE INDEX `uk_product_category`(`product_id` ASC, `category_id` ASC) USING BTREE COMMENT '商品与分类的唯一约束'
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci ROW_FORMAT = Dynamic;

-- ----------------------------
-- Table structure for products
-- ----------------------------
DROP TABLE IF EXISTS `products`;
CREATE TABLE `products`  (
                             `id` int NOT NULL AUTO_INCREMENT COMMENT '主键，自增,商品id',
                             `NAME` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT '商品名称',
                             `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL COMMENT '商品描述',
                             `picture` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '商品图片信息',
                             `price` decimal(10, 2) NOT NULL COMMENT '商品价格',
                             `stock` int NOT NULL COMMENT '库存数量',
                             `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
                             `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
                             PRIMARY KEY (`id`) USING BTREE
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci ROW_FORMAT = Dynamic;

-- ----------------------------
-- Table structure for users
-- ----------------------------
DROP TABLE IF EXISTS `users`;
CREATE TABLE `users`  (
                          `user_id` int NOT NULL AUTO_INCREMENT COMMENT '主键，自增，用户 ID',
                          `username` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '用户名，可空',
                          `email` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '邮箱，唯一',
                          `password_hash` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '密码哈希值',
                          `avatar_url` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL DEFAULT NULL COMMENT '头像图片 URL',
                          `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
                          `user_deleted` tinyint(1) NULL DEFAULT 0 COMMENT '用户是否已删除',
                          `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
                          `logout_at` timestamp NULL DEFAULT NULL COMMENT '登出时间',
                          `login_at` timestamp NULL DEFAULT NULL COMMENT '登录时间',
                          PRIMARY KEY (`user_id`) USING BTREE,
                          UNIQUE INDEX `email`(`email` ASC) USING BTREE
) ENGINE = InnoDB AUTO_INCREMENT = 2 CHARACTER SET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci ROW_FORMAT = Dynamic;

-- ----------------------------
-- Table structure for user_addresses
-- ----------------------------
DROP TABLE IF EXISTS `user_addresses`;
CREATE TABLE user_addresses (
    address_id INT AUTO_INCREMENT COMMENT '主键，自增，地址ID',
    user_id INT NOT NULL COMMENT '外键，关联到users表的user_id',
    detailed_address VARCHAR(255) NOT NULL COMMENT '详细地址',
    city VARCHAR(100) NOT NULL COMMENT '城市',
    province VARCHAR(100) DEFAULT NULL COMMENT '州/省',
    is_default BOOLEAN DEFAULT false COMMENT '是否默认地址',
    recipient_name VARCHAR(100) NOT NULL COMMENT '收件人姓名',
    phone_number VARCHAR(50) DEFAULT NULL COMMENT '联系电话',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (address_id),
    FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE
);

-- ----------------------------
-- Table structure for orders
-- ----------------------------
DROP TABLE IF EXISTS `orders`;
CREATE TABLE orders
(
    order_id        VARCHAR(36)  NOT NULL COMMENT '订单ID（业务主键）',
    pre_order_id    VARCHAR(36)  NOT NULL COMMENT '预订单ID（关联结算服务）',
    user_id         INT UNSIGNED NOT NULL COMMENT '用户ID',

    -- 支付信息
    payment_method  TINYINT COMMENT '支付方式（1-微信 2-支付宝）',
    transaction_id  VARCHAR(64) COMMENT '支付平台流水号',
    paid_at         BIGINT COMMENT '支付成功时间戳（秒）',

    -- 金额信息（与结算服务保持一致）
    original_amount BIGINT       NOT NULL COMMENT '订单原始金额（分）',
    discount_amount BIGINT       NOT NULL DEFAULT 0 COMMENT '优惠总金额（分）',
    payable_amount  BIGINT       NOT NULL COMMENT '应付金额（分）',
    paid_amount     BIGINT                DEFAULT NULL COMMENT '实收金额（分）',


    -- 状态管理
    order_status    TINYINT      NOT NULL COMMENT '订单状态：1-待支付 2-已支付 3-已发货 4-已完成 5-已取消...',
    payment_status  TINYINT      NOT NULL COMMENT '支付状态：0-未支付 1-支付中 2-已支付 3-已退款...',

    reason          VARCHAR(255) COMMENT '取消原因',
    expire_time     BIGINT       NOT NULL COMMENT '过期时间戳',
    created_at      TIMESTAMP             DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at      TIMESTAMP             DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (order_id),
    UNIQUE KEY idx_pre_order (pre_order_id),
    KEY idx_user_status (user_id, order_status),
    KEY idx_payment_time (paid_at)
) COMMENT ='主订单表';


-- ----------------------------
-- Table structure for order_addresses
-- ----------------------------
DROP TABLE IF EXISTS `order_addresses`;
CREATE TABLE order_addresses
(
    address_id       BIGINT UNSIGNED AUTO_INCREMENT,
    recipient_name   VARCHAR(100) NOT NULL COMMENT '收件人姓名',
    phone_number     VARCHAR(50)  DEFAULT NULL COMMENT '联系电话',
    province         VARCHAR(100) DEFAULT NULL COMMENT '州/省',
    city             VARCHAR(100) NOT NULL COMMENT '城市',
    detailed_address VARCHAR(255) NOT NULL COMMENT '详细地址',
    created_at       TIMESTAMP    DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at       TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (address_id),
    INDEX idx_recipient_name (recipient_name)
)

-- ----------------------------
-- Table structure for order_items
-- ----------------------------
DROP TABLE IF EXISTS `order_items`;
CREATE TABLE order_items
(
    item_id      BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id     VARCHAR(64)  NOT NULL COMMENT '关联订单号',

    -- 商品快照
    product_id   INT          NOT NULL COMMENT '商品ID',
    quantity     INT          NOT NULL COMMENT '购买数量',
    product_name VARCHAR(255) NOT NULL COMMENT '商品名称',
    product_desc TEXT COMMENT '规格描述',
    unit_price   BIGINT       NOT NULL COMMENT '单价(分)',

    FOREIGN KEY (order_id) REFERENCES orders (order_id),
    INDEX idx_order_product (order_id, product_id)
) COMMENT ='订单商品快照';

-- ----------------------------
-- Table structure for coupons
-- ----------------------------
DROP TABLE IF EXISTS `coupons`;
CREATE TABLE `coupons`
(
    `id`              varchar(36)  NOT NULL COMMENT '优惠券ID',
    `name`            varchar(100) NOT NULL COMMENT '券名称',
    `type`            tinyint      NOT NULL COMMENT '类型：1-满减 2-折扣 3-立减',
    `value`           BIGINT       NOT NULL COMMENT '优惠值（根据类型：分/百分比）', -- 满减立减通用,折扣通用（例如：满200减50存50，8折存80）
    `min_amount`      BIGINT                DEFAULT 0 COMMENT '最低消费金额（分）', -- 满减立减通用,折扣通用
    `start_time`      timestamp    NOT NULL COMMENT '有效期开始',
    `end_time`        timestamp    NOT NULL COMMENT '有效期结束',
    `status`          tinyint      NOT NULL DEFAULT 1 COMMENT '状态：0-禁用 1-启用',
    `total_count`     int          NOT NULL COMMENT '发行总量',
    `remaining_count` int          NOT NULL COMMENT '剩余数量',
    `created_at`      timestamp          DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at`      timestamp          DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    KEY `idx_time` (`start_time`, `end_time`),
    KEY `idx_status` (`status`)
) COMMENT ='优惠券基本信息';

-- ----------------------------
-- Table structure for user_coupons
-- ----------------------------
DROP TABLE IF EXISTS `user_coupons`;
-- 用户优惠券关联表
CREATE TABLE `user_coupons`
(
    `id`        int unsigned  NOT NULL AUTO_INCREMENT,
    `user_id`   int unsigned             NOT NULL COMMENT '用户ID',
    `coupon_id` varchar(36)              NOT NULL COMMENT '优惠券ID',
    `status`    tinyint                  NOT NULL DEFAULT 0 COMMENT '状态：0-未使用 1-已使用 2-已过期',
    `order_id`  varchar(36)                       DEFAULT NULL COMMENT '使用的订单ID',

    `used_at`   TIMESTAMP                         DEFAULT NULL COMMENT '使用时间',
    created_at  TIMESTAMP                         DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at  TIMESTAMP                         DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_user_coupon` (`user_id`, `coupon_id`),
    KEY `idx_user_status` (`user_id`, `status`),
    KEY `idx_order` (`order_id`)
) COMMENT ='用户持有优惠券';

-- ----------------------------
-- Table structure for coupon_usage
-- ----------------------------
DROP TABLE IF EXISTS `coupon_usage`;
-- 优惠券使用记录表
CREATE TABLE `coupon_usage`
(
    `id`              int unsigned NOT NULL AUTO_INCREMENT,
    `order_id`        varchar(36)              NOT NULL COMMENT '关联的订单ID',
    `coupon_id`       varchar(36)              NOT NULL COMMENT '优惠券ID',
    `user_id`         int unsigned             NOT NULL COMMENT '用户ID',
    `coupon_type`     tinyint                  NOT NULL COMMENT '当时优惠券类型：1-满减 2-折扣 3-立减',
    `origin_value`    BIGINT                      NOT NULL COMMENT '当时优惠值（根据类型：分/百分比）',
    `discount_amount` BIGINT                      NOT NULL COMMENT '抵扣金额（分）',
    `applied_at`      TIMESTAMP                NOT NULL COMMENT '应用时间',
    PRIMARY KEY (`id`),
    KEY `idx_order` (`order_id`),
    KEY `idx_user` (`user_id`)
) COMMENT ='优惠券使用明细';



-- ----------------------------
-- Table structure for inventory
-- ----------------------------
DROP TABLE IF EXISTS `inventory`;
CREATE TABLE inventory
(
    product_id INT, -- 与商品服务共享同一ID
    total      INT NOT NULL,
    sold       INT NOT NULL,
    primary key (product_id)
);

-- ----------------------------
-- Table structure for audit
-- ----------------------------
DROP TABLE IF EXISTS `audit`;
create table `audit`
(
    id           int UNSIGNED auto_increment comment '主键',
    user_id      int UNSIGNED not null comment '用户id',
    action_type  varchar(64)  not null comment '操作类型',
    action_desc  text comment '操作描述',
    old_data     json comment '旧数据',
    new_data     json comment '新数据',
    target_table varchar(64)  not null comment '目标表',
    target_id    int UNSIGNED not null comment '目标id',
    client_ip    varchar(45)  not null comment 'ip地址',
    trace_id     varchar(36)  not null comment 'traceid', -- 用于关联跟踪 （但是可能不到64字长）
    span_id      varchar(36)  not null comment 'spanid',  -- 用于关联跟踪
    created_at   TIMESTAMP default CURRENT_TIMESTAMP COMMENT '创建时间',

    primary key (id),
    UNIQUE idx_trace (trace_id),
    INDEX idx_user (user_id),
    INDEX idx_action (action_type),
    INDEX idx_target (target_table, target_id),
    INDEX idx_time (created_at)
);

-- ----------------------------
-- Table structure for payments
-- ----------------------------
DROP TABLE IF EXISTS `payments`;
CREATE TABLE payments
(
    payment_id      VARCHAR(36)  NOT NULL COMMENT '支付单ID（UUID）',
    pre_order_id    VARCHAR(36)  NOT NULL COMMENT '预订单ID（外键）',
    order_id        VARCHAR(36)  DEFAULT NULL COMMENT '关联订单ID（支付成功后更新）',

    original_amount BIGINT       NOT NULL COMMENT '订单原价（单位：分）',
    paid_amount     BIGINT       DEFAULT NULL COMMENT '实付金额（分）',

    -- 支付信息
    payment_method  VARCHAR(20)  NOT NULL COMMENT '支付渠道（wx_pay/alipay）',
    transaction_id  VARCHAR(64)  DEFAULT NULL COMMENT '支付平台交易号',
    pay_url         VARCHAR(512) NOT NULL COMMENT '支付跳转链接',
    expire_time     BIGINT       NOT NULL COMMENT '支付链接过期时间戳（秒）',

    status          TINYINT      NOT NULL COMMENT '支付状态（0-未定义 1-待支付 2-已支付...）',

    created_at      TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间（毫秒精度）',
    updated_at      TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    paid_at         BIGINT       DEFAULT NULL COMMENT '支付成功时间戳（秒）',

    PRIMARY KEY (payment_id),
    UNIQUE KEY uk_idempotency (order_id),
    INDEX idx_pre_order (pre_order_id),
    INDEX idx_order (order_id),
    INDEX idx_status_method (status, payment_method),
    INDEX idx_create_time (created_at)
)

-- ----------------------------
-- Table structure for checkouts
-- ----------------------------
DROP TABLE IF EXISTS `checkouts`;
CREATE TABLE checkouts
(
    pre_order_id    VARCHAR(36)  NOT NULL COMMENT '预订单ID',
    user_id         INT UNSIGNED NOT NULL COMMENT '用户ID',
    coupon_id       VARCHAR(36) COMMENT '使用优惠券ID列表（支持多券）',
    status          TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '状态：0-预占中 1-已确认 2-已取消 3-已过期',
    expire_time     BIGINT       NOT NULL COMMENT '过期时间戳（秒）',
    created_at      TIMESTAMP(3)          DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间（毫秒精度）',
    updated_at      TIMESTAMP(3)          DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    PRIMARY KEY (pre_order_id),
    INDEX idx_user_status (user_id, status),
    INDEX idx_expire (expire_time)
) COMMENT ='预订单主表';

DROP TABLE IF EXISTS `checkout_items`;
CREATE TABLE checkout_items
(
    pre_order_id    VARCHAR(36)  NOT NULL COMMENT '预订单ID',
    product_id   INT UNSIGNED NOT NULL COMMENT '商品ID',
    quantity     INT UNSIGNED NOT NULL COMMENT '数量',
    price        BIGINT       NOT NULL COMMENT '当时单价（分）',
    product_name VARCHAR(255) NOT NULL COMMENT '商品名称',
    product_desc VARCHAR(255) NOT NULL COMMENT '商品描述',
    created_at   TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (pre_order_id),
    INDEX idx_product (product_id)
) COMMENT ='订单商品快照';

SET FOREIGN_KEY_CHECKS = 1;
