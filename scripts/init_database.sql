-- 文件存储系统数据库初始化脚本
-- 创建 files 表用于存储文件元数据信息

-- 如果表已存在则删除（仅用于开发环境）
DROP TABLE IF EXISTS files;

-- 创建 files 表
CREATE TABLE files (
    id BIGSERIAL PRIMARY KEY,                          -- 文件ID，自增主键
    file_name VARCHAR(255) NOT NULL,                   -- 文件名称
    file_size BIGINT NOT NULL,                         -- 文件大小（字节）
    local_path VARCHAR(500) NOT NULL,                  -- 本地存储路径
    storage_nodes TEXT,                                -- 存储该文件的节点ID列表，逗号分隔
    storage_add VARCHAR(500),                          -- 文件目录树路径，格式: "1-2-3"
    owner_id VARCHAR(100),                             -- 文件所有者ID
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),       -- 创建时间
    updated_at TIMESTAMP DEFAULT NOW()                 -- 更新时间
);

-- 创建索引以提高查询性能
CREATE INDEX idx_files_file_name ON files(file_name);
CREATE INDEX idx_files_owner_id ON files(owner_id);
CREATE INDEX idx_files_created_at ON files(created_at);

-- 创建更新时间自动更新触发器
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

-- 创建目录树表
DROP TABLE IF EXISTS directory_tree CASCADE;

CREATE TABLE directory_tree (
    id BIGSERIAL PRIMARY KEY,                          -- 目录节点ID，自增主键
    name VARCHAR(255) NOT NULL,                        -- 文件名或目录名
    parent_id BIGINT REFERENCES directory_tree(id) ON DELETE CASCADE, -- 父目录ID，NULL表示根目录
    is_dir BOOLEAN NOT NULL DEFAULT false,             -- 是否为目录
    file_id BIGINT REFERENCES files(id) ON DELETE SET NULL, -- 关联的文件ID（仅对文件节点）
    owner_id VARCHAR(100),                             -- 所有者ID
    created_at TIMESTAMP NOT NULL DEFAULT NOW()        -- 创建时间
);

-- 创建索引
CREATE INDEX idx_directory_tree_parent_id ON directory_tree(parent_id);
CREATE INDEX idx_directory_tree_file_id ON directory_tree(file_id);
CREATE INDEX idx_directory_tree_owner_id ON directory_tree(owner_id);

-- 插入测试数据（可选）
-- INSERT INTO files (file_name, file_size, local_path, storage_nodes, storage_add, owner_id)
-- VALUES ('test.txt', 1024, 'FileStorage/test.txt', '1,2,3', '1-2-3', 'user001');

COMMENT ON TABLE files IS '文件存储元数据表';
COMMENT ON COLUMN files.id IS '文件唯一标识符，自增主键';
COMMENT ON COLUMN files.file_name IS '文件名称';
COMMENT ON COLUMN files.file_size IS '文件大小（字节）';
COMMENT ON COLUMN files.local_path IS '文件在服务器上的本地存储路径';
COMMENT ON COLUMN files.storage_nodes IS '存储该文件的所有节点ID列表，使用逗号分隔';
COMMENT ON COLUMN files.storage_add IS '文件目录树路径，格式为"父ID-子ID-文件ID"';
COMMENT ON COLUMN files.owner_id IS '文件所有者的用户ID或客户端标识';
COMMENT ON COLUMN files.created_at IS '文件创建时间';
COMMENT ON COLUMN files.updated_at IS '文件最后更新时间';

COMMENT ON TABLE directory_tree IS '文件目录树表';
COMMENT ON COLUMN directory_tree.id IS '目录节点唯一标识符，自增主键';
COMMENT ON COLUMN directory_tree.name IS '文件名或目录名';
COMMENT ON COLUMN directory_tree.parent_id IS '父目录ID，NULL表示根目录';
COMMENT ON COLUMN directory_tree.is_dir IS '是否为目录（true）或文件（false）';
COMMENT ON COLUMN directory_tree.file_id IS '关联的文件ID，仅对文件节点有效';
COMMENT ON COLUMN directory_tree.owner_id IS '所有者的用户ID';
COMMENT ON COLUMN directory_tree.created_at IS '创建时间';
