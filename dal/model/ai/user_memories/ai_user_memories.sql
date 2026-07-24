CREATE TABLE `ai_user_memories` (
  `id` varchar(64) NOT NULL COMMENT 'ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `memory_key` varchar(128) NOT NULL COMMENT '用户内稳定记忆键',
  `memory_type` varchar(32) NOT NULL COMMENT 'instruction/preference/price/profile_fact',
  `content` text NOT NULL COMMENT '记忆内容',
  `confidence` decimal(5,4) NOT NULL DEFAULT 0.0000 COMMENT '置信度',
  `source` varchar(32) NOT NULL DEFAULT 'explicit' COMMENT 'explicit/inferred',
  `source_message_id` varchar(64) NOT NULL DEFAULT '' COMMENT '来源消息ID',
  `status` varchar(16) NOT NULL DEFAULT 'active' COMMENT 'active/superseded/deleted/expired',
  `expires_at` datetime DEFAULT NULL COMMENT '过期时间',
  `last_confirmed_at` datetime DEFAULT NULL COMMENT '最近确认时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_memory_key` (`user_id`, `memory_key`),
  KEY `idx_user_type_updated` (`user_id`, `memory_type`, `updated_at`),
  KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
