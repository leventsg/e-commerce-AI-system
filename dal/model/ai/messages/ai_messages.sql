CREATE TABLE `ai_messages` (
  `id` varchar(64) NOT NULL COMMENT '消息ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `role` varchar(16) NOT NULL COMMENT 'user/assistant/tool',
  `content` text NOT NULL COMMENT '消息内容',
  `metadata` json DEFAULT NULL COMMENT '扩展信息',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
