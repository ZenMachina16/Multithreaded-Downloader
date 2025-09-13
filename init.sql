-- Initialize the downloads database
CREATE DATABASE downloads;

-- Connect to the downloads database
\c downloads;

-- Create downloads table
CREATE TABLE IF NOT EXISTS downloads (
    id VARCHAR(36) PRIMARY KEY,
    url TEXT NOT NULL,
    output_path TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    bytes_downloaded BIGINT DEFAULT 0,
    total_bytes BIGINT DEFAULT 0,
    threads INTEGER DEFAULT 4,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create index on status for faster queries
CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);

-- Create index on created_at for time-based queries
CREATE INDEX IF NOT EXISTS idx_downloads_created_at ON downloads(created_at);
