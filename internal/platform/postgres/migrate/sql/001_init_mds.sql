CREATE TABLE IF NOT EXISTS mds_inodes (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL DEFAULT '',
    parent_id TEXT NOT NULL DEFAULT '',
    file_id TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    permissions BIGINT NOT NULL DEFAULT 0,
    owner_name TEXT NOT NULL DEFAULT '',
    group_name TEXT NOT NULL DEFAULT '',
    link_count BIGINT NOT NULL DEFAULT 0,
    generation BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    accessed_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_inodes_namespace_path_uq
    ON mds_inodes(namespace, path);

CREATE UNIQUE INDEX IF NOT EXISTS mds_inodes_namespace_parent_name_uq
    ON mds_inodes(namespace, parent_id, name);

CREATE TABLE IF NOT EXISTS mds_files (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL DEFAULT '',
    inode_id TEXT NOT NULL,
    parent_inode_id TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    name TEXT NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    stored_size BIGINT NOT NULL DEFAULT 0,
    chunk_size BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    storage_class TEXT NOT NULL DEFAULT '',
    primary_node_id TEXT NOT NULL DEFAULT '',
    secondary_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    latest_upload_session_id TEXT NOT NULL DEFAULT '',
    checksum_algorithm TEXT NOT NULL DEFAULT '',
    checksum_value TEXT NOT NULL DEFAULT '',
    checksum_verified BOOLEAN NOT NULL DEFAULT FALSE,
    checksum_verified_at TIMESTAMPTZ NULL,
    desired_replica_count INTEGER NOT NULL DEFAULT 0,
    minimum_replica_count INTEGER NOT NULL DEFAULT 0,
    current_replica_count INTEGER NOT NULL DEFAULT 0,
    user_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT mds_files_inode_fk FOREIGN KEY (inode_id) REFERENCES mds_inodes(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_files_inode_id_uq
    ON mds_files(inode_id);

CREATE TABLE IF NOT EXISTS mds_upload_sessions (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    upload_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    expected_size BIGINT NOT NULL DEFAULT 0,
    chunk_size BIGINT NOT NULL DEFAULT 0,
    confirmed_offset BIGINT NOT NULL DEFAULT 0,
    next_offset BIGINT NOT NULL DEFAULT 0,
    last_persisted_chunk_id TEXT NOT NULL DEFAULT '',
    expected_checksum_algorithm TEXT NOT NULL DEFAULT '',
    expected_checksum_value TEXT NOT NULL DEFAULT '',
    expected_checksum_verified BOOLEAN NOT NULL DEFAULT FALSE,
    expected_checksum_verified_at TIMESTAMPTZ NULL,
    verified_checksum_algorithm TEXT NOT NULL DEFAULT '',
    verified_checksum_value TEXT NOT NULL DEFAULT '',
    verified_checksum_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verified_checksum_verified_at TIMESTAMPTZ NULL,
    retry_attempt INTEGER NOT NULL DEFAULT 0,
    retry_max_attempts INTEGER NOT NULL DEFAULT 0,
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    last_failed_offset BIGINT NOT NULL DEFAULT 0,
    last_failed_chunk TEXT NOT NULL DEFAULT '',
    last_failure_at TIMESTAMPTZ NULL,
    next_retry_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    client_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    transport_attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT mds_upload_sessions_file_fk FOREIGN KEY (file_id) REFERENCES mds_files(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_upload_sessions_active_file_uq
    ON mds_upload_sessions(file_id)
    WHERE status IN ('pending', 'active', 'paused', 'retrying', 'verifying');

CREATE TABLE IF NOT EXISTS mds_chunks (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    chunk_index BIGINT NOT NULL,
    chunk_offset BIGINT NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    checksum_algorithm TEXT NOT NULL DEFAULT '',
    checksum_value TEXT NOT NULL DEFAULT '',
    checksum_verified BOOLEAN NOT NULL DEFAULT FALSE,
    checksum_verified_at TIMESTAMPTZ NULL,
    desired_replica_count INTEGER NOT NULL DEFAULT 0,
    minimum_replica_count INTEGER NOT NULL DEFAULT 0,
    current_replica_count INTEGER NOT NULL DEFAULT 0,
    replica_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ NULL,
    last_error_code TEXT NOT NULL DEFAULT '',
    CONSTRAINT mds_chunks_file_fk FOREIGN KEY (file_id) REFERENCES mds_files(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_chunks_file_index_uq
    ON mds_chunks(file_id, chunk_index);

CREATE TABLE IF NOT EXISTS mds_nodes (
    id TEXT PRIMARY KEY,
    address TEXT NOT NULL DEFAULT '',
    rack TEXT NOT NULL DEFAULT '',
    zone TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT '',
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    capacity BIGINT NOT NULL DEFAULT 0,
    used BIGINT NOT NULL DEFAULT 0,
    healthy BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS mds_chunk_replicas (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    chunk_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    role TEXT NOT NULL,
    state TEXT NOT NULL,
    checksum_algorithm TEXT NOT NULL DEFAULT '',
    checksum_value TEXT NOT NULL DEFAULT '',
    checksum_verified BOOLEAN NOT NULL DEFAULT FALSE,
    checksum_verified_at TIMESTAMPTZ NULL,
    stored_size BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ NULL,
    CONSTRAINT mds_chunk_replicas_chunk_fk FOREIGN KEY (chunk_id) REFERENCES mds_chunks(id) ON DELETE CASCADE,
    CONSTRAINT mds_chunk_replicas_node_fk FOREIGN KEY (node_id) REFERENCES mds_nodes(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_chunk_replicas_chunk_node_uq
    ON mds_chunk_replicas(chunk_id, node_id);

CREATE TABLE IF NOT EXISTS mds_file_placements (
    file_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    replica_role TEXT NOT NULL,
    replica_state TEXT NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    chunk_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    stored_size BIGINT NOT NULL DEFAULT 0,
    checksum_state TEXT NOT NULL DEFAULT '',
    last_sync_at TIMESTAMPTZ NULL,
    PRIMARY KEY (file_id, node_id),
    CONSTRAINT mds_file_placements_file_fk FOREIGN KEY (file_id) REFERENCES mds_files(id) ON DELETE CASCADE,
    CONSTRAINT mds_file_placements_node_fk FOREIGN KEY (node_id) REFERENCES mds_nodes(id)
);

CREATE TABLE IF NOT EXISTS mds_replica_plans (
    id TEXT PRIMARY KEY,
    plan_type TEXT NOT NULL,
    chunk_id TEXT NOT NULL,
    file_id TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    required_bytes BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_message TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT mds_replica_plans_chunk_fk FOREIGN KEY (chunk_id) REFERENCES mds_chunks(id) ON DELETE CASCADE,
    CONSTRAINT mds_replica_plans_file_fk FOREIGN KEY (file_id) REFERENCES mds_files(id) ON DELETE CASCADE,
    CONSTRAINT mds_replica_plans_source_node_fk FOREIGN KEY (source_node_id) REFERENCES mds_nodes(id),
    CONSTRAINT mds_replica_plans_target_node_fk FOREIGN KEY (target_node_id) REFERENCES mds_nodes(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS mds_replica_plans_active_target_uq
    ON mds_replica_plans(chunk_id, plan_type, target_node_id)
    WHERE state NOT IN ('done', 'failed');
