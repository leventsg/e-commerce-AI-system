CREATE TABLE `ai_conversation_summaries` (
  `id` varchar(64) NOT NULL COMMENT '摘要ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `covered_until_created_at` datetime(3) NOT NULL COMMENT '已覆盖消息时间水位',
  `covered_until_message_id` varchar(64) NOT NULL COMMENT '已覆盖消息ID水位',
  `summary` text NOT NULL COMMENT '会话摘要',
  `key_facts` json NOT NULL COMMENT '稳定关键事实',
  `open_tasks` json NOT NULL COMMENT '未完成事项',
  `token_count` int unsigned NOT NULL DEFAULT 0 COMMENT '摘要估算Token',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_conversation_watermark` (`user_id`, `conversation_id`, `covered_until_created_at`, `covered_until_message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
