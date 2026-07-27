CREATE TABLE `ai_user_profiles` (
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
