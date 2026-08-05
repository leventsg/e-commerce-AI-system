CREATE TABLE `ai_agent_runs` (
  `run_id` varchar(64) NOT NULL COMMENT 'Agent run ID',
  `conversation_id` varchar(64) NOT NULL DEFAULT '' COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT '用户ID',
  `status` varchar(16) NOT NULL DEFAULT 'interrupted' COMMENT 'running/interrupted/completed/failed/expired',
  `checkpoint_id` varchar(128) NOT NULL COMMENT 'Eino checkpoint ID',
  `checkpoint_blob` longblob NOT NULL COMMENT 'Eino checkpoint payload',
  `task_state` json DEFAULT NULL COMMENT '任务状态',
  `idempotency_key` varchar(128) NOT NULL DEFAULT '' COMMENT '幂等键',
  `expires_at` datetime NOT NULL COMMENT '过期时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`run_id`),
  UNIQUE KEY `uk_checkpoint_id` (`checkpoint_id`),
  KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
