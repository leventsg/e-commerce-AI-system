CREATE TABLE `ai_confirmations` (
  `id` varchar(64) NOT NULL COMMENT '确认ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `tool_name` varchar(64) NOT NULL COMMENT '工具名称',
  `arguments` json NOT NULL COMMENT '待执行参数',
  `summary` varchar(512) NOT NULL COMMENT '确认摘要',
  `status` varchar(16) NOT NULL COMMENT 'pending/approved/rejected/expired/executed/failed',
  `expires_at` datetime NOT NULL COMMENT '过期时间',
  `executed_at` datetime DEFAULT NULL COMMENT '执行时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
