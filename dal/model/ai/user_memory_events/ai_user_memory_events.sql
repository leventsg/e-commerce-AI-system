CREATE TABLE `ai_user_memory_events` (
  `id` varchar(64) NOT NULL COMMENT '事件ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `type` varchar(32) NOT NULL COMMENT 'milestone/event',
  `event_date` datetime NOT NULL COMMENT '事件日期',
  `summary` varchar(512) NOT NULL COMMENT '事件摘要',
  `keywords` json NOT NULL COMMENT '关键词',
  `status` varchar(16) NOT NULL DEFAULT 'active' COMMENT 'active/deleted',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status_date` (`user_id`, `status`, `event_date`),
  KEY `idx_user_type_date` (`user_id`, `type`, `event_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
