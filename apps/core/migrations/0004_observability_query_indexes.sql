-- Task 5.4：按快照和稳定游标支持请求记录查询；不新增或读取费用数据。
CREATE INDEX IF NOT EXISTS idx_requests_created_id ON requests(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_requests_provider_snapshot_created_id ON requests(provider_slug_snapshot, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_requests_public_model_snapshot_created_id ON requests(public_model_snapshot, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_requests_status_created_id ON requests(status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_requests_protocol_created_id ON requests(source_protocol, created_at DESC, id DESC);
