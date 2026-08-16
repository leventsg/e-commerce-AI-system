CREATE TABLE `ai_tool_calls` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `tool_call_id` varchar(128) NOT NULL DEFAULT '' COMMENT '模型工具调用ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `tool_name` varchar(64) NOT NULL COMMENT '工具名称',
  `arguments` json NOT NULL COMMENT '工具参数',
  `result` json NOT NULL COMMENT '真实工具返回JSON',
  `status` varchar(16) NOT NULL COMMENT 'success/failed',
  `error_message` varchar(512) NOT NULL DEFAULT '',
  `latency_ms` bigint NOT NULL DEFAULT 0,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_conversation_tool_call` (`conversation_id`, `tool_call_id`),
  KEY `idx_user_tool_created` (`user_id`, `tool_name`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
