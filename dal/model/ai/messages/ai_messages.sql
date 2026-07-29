CREATE TABLE `ai_messages` (
  `seq` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '消息自增序号',
  `msg_id` varchar(64) NOT NULL COMMENT '消息ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `role` varchar(16) NOT NULL COMMENT 'user/assistant/tool',
  `content` text NOT NULL COMMENT '消息内容',
  `metadata` json DEFAULT NULL COMMENT '扩展信息',
  `client_message_id` varchar(128) DEFAULT NULL COMMENT '前端生成的用户消息幂等ID',
  `dedupe_client_message_id` varchar(128) GENERATED ALWAYS AS (case when `role` = 'user' then `client_message_id` else NULL end) STORED COMMENT '仅用户消息参与幂等唯一约束',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`seq`),
  UNIQUE KEY `uk_msg_id` (`msg_id`),
  UNIQUE KEY `uk_user_client_message` (`user_id`, `dedupe_client_message_id`),
  KEY `idx_conversation_seq` (`conversation_id`, `seq`),
  KEY `idx_user_seq` (`user_id`, `seq`),
  KEY `idx_user_conversation_client_role_seq` (`user_id`, `conversation_id`, `client_message_id`, `role`, `seq`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
