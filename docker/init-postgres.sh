#!/bin/bash
set -e

# 修改 pg_hba.conf 以允许所有连接使用 trust 认证
echo "host all all all trust" >> /var/lib/postgresql/data/pg_hba.conf

# 重新加载配置
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    SELECT pg_reload_conf();
EOSQL
