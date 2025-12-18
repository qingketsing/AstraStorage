-- File Storage System Database Initialization Script
-- Creates tables for file metadata and directory tree

-- Drop tables if they exist (for development environment)
DROP TABLE IF EXISTS directory_tree CASCADE;
DROP TABLE IF EXISTS files CASCADE;

-- Create files table
CREATE TABLE files (
    id BIGSERIAL PRIMARY KEY,
    file_name VARCHAR(255) NOT NULL,
    file_size BIGINT NOT NULL,
    local_path VARCHAR(500) NOT NULL,
    storage_nodes TEXT,
    storage_add VARCHAR(500),
    owner_id VARCHAR(100),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create indexes for files table
CREATE INDEX idx_files_file_name ON files(file_name);
CREATE INDEX idx_files_owner_id ON files(owner_id);
CREATE INDEX idx_files_created_at ON files(created_at);

-- Create update timestamp trigger
CREATE OR REPLACE FUNCTION update_modified_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_files_updated_at
BEFORE UPDATE ON files
FOR EACH ROW
EXECUTE FUNCTION update_modified_column();

-- Create directory_tree table
CREATE TABLE directory_tree (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id BIGINT REFERENCES directory_tree(id) ON DELETE CASCADE,
    is_dir BOOLEAN NOT NULL DEFAULT false,
    file_id BIGINT REFERENCES files(id) ON DELETE SET NULL,
    owner_id VARCHAR(100),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create indexes for directory_tree table
CREATE INDEX idx_directory_tree_parent_id ON directory_tree(parent_id);
CREATE INDEX idx_directory_tree_file_id ON directory_tree(file_id);
CREATE INDEX idx_directory_tree_owner_id ON directory_tree(owner_id);
