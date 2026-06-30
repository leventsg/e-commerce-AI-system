CREATE TABLE `ai_tool_calls` (
  `id` varchar(64) NOT NULL COMMENT '调用ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `tool_name` varchar(64) NOT NULL COMMENT '工具名称',
  `arguments` json NOT NULL COMMENT '工具参数',
  `result_summary` text COMMENT '结果摘要',
  `status` varchar(16) NOT NULL COMMENT 'success/failed',
  `error_message` varchar(512) NOT NULL DEFAULT '',
  `latency_ms` bigint NOT NULL DEFAULT 0,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_user_tool_created` (`user_id`, `tool_name`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
