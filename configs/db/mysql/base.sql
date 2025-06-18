CREATE TABLE `active_versions` (
    `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
    `tenant` varchar(50) NOT NULL,
    `name` varchar(50) NOT NULL,
    `version` bigint(20) NOT NULL,
    `updated_at` datetime(3) NOT NULL,
    `deleted_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_tenant_name` (`tenant`,`name`),
    KEY `idx_active_versions_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `mcp_config_versions` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `name` varchar(50) DEFAULT NULL,
    `tenant` varchar(50) DEFAULT NULL,
    `version` bigint(20) DEFAULT NULL,
    `action_type` longtext NOT NULL,
    `created_by` longtext,
    `created_at` datetime(3) DEFAULT NULL,
    `routers` text,
    `servers` text,
    `tools` text,
    `mcp_servers` text,
    `hash` longtext NOT NULL,
    `deleted_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_name_tenant_version` (`name`,`tenant`,`version`),
    KEY `idx_mcp_config_versions_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `mcp_configs` (
    `id` bigint(20) NOT NULL AUTO_INCREMENT,
    `name` varchar(50) DEFAULT NULL,
    `tenant` varchar(50) DEFAULT '',
    `created_at` datetime(3) DEFAULT NULL,
    `updated_at` datetime(3) DEFAULT NULL,
    `routers` text,
    `servers` text,
    `tools` text,
    `mcp_servers` text,
    `deleted_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_name_tenant` (`tenant`,`name`),
    KEY `idx_mcp_configs_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `messages` (
    `id` varchar(64) NOT NULL,
    `session_id` varchar(64) DEFAULT NULL,
    `content` text,
    `sender` varchar(50) DEFAULT NULL,
    `timestamp` datetime(3) DEFAULT NULL,
    `tool_calls` text,
    `tool_result` text,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_messages_id` (`id`),
    KEY `idx_messages_timestamp` (`timestamp`),
    KEY `idx_messages_session_id` (`session_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `sessions` (
    `id` varchar(64) NOT NULL,
    `created_at` datetime(3) DEFAULT NULL,
    `title` varchar(255) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_sessions_id` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `tenants` (
    `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
    `name` varchar(50) DEFAULT NULL,
    `prefix` varchar(50) DEFAULT NULL,
    `description` varchar(255) DEFAULT NULL,
    `is_active` tinyint(1) NOT NULL DEFAULT '1',
    `created_at` datetime(3) DEFAULT NULL,
    `updated_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_tenants_name` (`name`),
    UNIQUE KEY `idx_tenants_prefix` (`prefix`)
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `user_tenants` (
    `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
    `user_id` bigint(20) unsigned NOT NULL,
    `tenant_id` bigint(20) unsigned NOT NULL,
    `created_at` datetime(3) DEFAULT NULL,
    `updated_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_user_tenant` (`user_id`,`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `users` (
    `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
    `username` varchar(50) DEFAULT NULL,
    `password` longtext NOT NULL,
    `role` varchar(191) NOT NULL DEFAULT 'normal',
    `is_active` tinyint(1) NOT NULL DEFAULT '1',
    `created_at` datetime(3) DEFAULT NULL,
    `updated_at` datetime(3) DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_users_username` (`username`)
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;