CREATE TABLE `ai_user_memories` (
  `id` varchar(64) NOT NULL COMMENT '记忆ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `memory_type` varchar(32) NOT NULL COMMENT 'preference/category/price',
  `content` text NOT NULL COMMENT '记忆内容',
  `confidence` decimal(5,4) NOT NULL DEFAULT 0.0000 COMMENT '置信度',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_type_updated` (`user_id`, `memory_type`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
