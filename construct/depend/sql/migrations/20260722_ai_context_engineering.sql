CREATE TABLE IF NOT EXISTS `ai_conversation_summaries` (
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

ALTER TABLE `ai_user_memories`
  ADD COLUMN `memory_key` varchar(128) NOT NULL COMMENT '用户内稳定记忆键' AFTER `user_id`,
  ADD COLUMN `source` varchar(32) NOT NULL DEFAULT 'explicit' COMMENT 'explicit/inferred' AFTER `confidence`,
  ADD COLUMN `source_message_id` varchar(64) NOT NULL DEFAULT '' COMMENT '来源消息ID' AFTER `source`,
  ADD COLUMN `status` varchar(16) NOT NULL DEFAULT 'active' COMMENT 'active/superseded/deleted/expired' AFTER `source_message_id`,
  ADD COLUMN `expires_at` datetime DEFAULT NULL COMMENT '过期时间' AFTER `status`,
  ADD COLUMN `last_confirmed_at` datetime DEFAULT NULL COMMENT '最近确认时间' AFTER `expires_at`,
  ADD UNIQUE KEY `uk_user_memory_key` (`user_id`, `memory_key`),
  ADD KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`);

CREATE TABLE IF NOT EXISTS `ai_user_profiles` (
  `id` varchar(64) NOT NULL COMMENT '画像ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `profile_json` json NOT NULL COMMENT '聊天来源用户画像JSON',
  `version` int unsigned NOT NULL DEFAULT 1 COMMENT '画像版本',
  `source` varchar(32) NOT NULL DEFAULT 'ai_chat' COMMENT '来源',
  `status` varchar(32) NOT NULL DEFAULT 'active' COMMENT 'active/deleted',
  `last_event_id` varchar(64) NOT NULL DEFAULT '' COMMENT '最近画像更新事件ID',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_user_profile` (`user_id`),
  KEY `idx_user_status` (`user_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE `ai_confirmations`
  ADD COLUMN `run_id` varchar(64) NOT NULL DEFAULT '' COMMENT 'Agent run ID' AFTER `status`,
  ADD COLUMN `checkpoint_id` varchar(128) NOT NULL DEFAULT '' COMMENT 'Eino checkpoint ID' AFTER `run_id`,
  ADD COLUMN `interrupt_id` varchar(128) NOT NULL DEFAULT '' COMMENT 'Eino interrupt ID' AFTER `checkpoint_id`,
  ADD KEY `idx_checkpoint_interrupt` (`checkpoint_id`, `interrupt_id`);

CREATE TABLE IF NOT EXISTS `ai_agent_runs` (
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
