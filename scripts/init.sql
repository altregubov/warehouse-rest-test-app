-- PostgreSQL Database Initialization & Migration Script for Warehouse REST API
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Users Table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'user')),
    balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00 CHECK (balance >= 0),
    allowed_categories TEXT[] DEFAULT '{}',
    allowed_manufacturers TEXT[] DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE NULL
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP WITH TIME ZONE NULL;

-- 2. Products Table
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    category VARCHAR(100) NOT NULL,
    manufacturer VARCHAR(100) NOT NULL,
    model VARCHAR(150) NOT NULL,
    price NUMERIC(12, 2) NOT NULL CHECK (price >= 0),
    stock_quantity INTEGER NOT NULL CHECK (stock_quantity >= 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. Orders Table
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    total_price NUMERIC(12, 2) NOT NULL CHECK (total_price >= 0),
    status VARCHAR(50) NOT NULL DEFAULT 'CREATED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE orders ADD COLUMN IF NOT EXISTS status VARCHAR(50) NOT NULL DEFAULT 'CREATED';
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_user_id_fkey;
ALTER TABLE orders ADD CONSTRAINT orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_manufacturer ON products(manufacturer);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);

-- 4. Idempotency Keys Table
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key VARCHAR(255) PRIMARY KEY,
    request_path VARCHAR(255) NOT NULL,
    request_method VARCHAR(20) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    status_code INTEGER NULL,
    response_body TEXT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'STARTED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE DEFAULT (CURRENT_TIMESTAMP + INTERVAL '24 hours')
);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);

-- 5. Seed Data
-- Passwords:
-- admin123: $2a$10$RUP6Lknor1aYWPyngT8WjOkiwFpkibEmguyv7e5gTbKae/hn5OAKW
-- user123:  $2a$10$pp.NmQ27Jz1aeJiA2fPJTu79LQhY9v/Pxh02nSJvQ5k1cpq7BFAD2

INSERT INTO users (id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers)
VALUES
    ('a0000000-0000-0000-0000-000000000001', 'admin', '$2a$10$RUP6Lknor1aYWPyngT8WjOkiwFpkibEmguyv7e5gTbKae/hn5OAKW', 'admin', 0.00, '{}', '{}'),
    ('b0000000-0000-0000-0000-000000000002', 'userA', '$2a$10$pp.NmQ27Jz1aeJiA2fPJTu79LQhY9v/Pxh02nSJvQ5k1cpq7BFAD2', 'user', 5000.00, '{}', '{}'),
    ('b0000000-0000-0000-0000-000000000003', 'userB', '$2a$10$pp.NmQ27Jz1aeJiA2fPJTu79LQhY9v/Pxh02nSJvQ5k1cpq7BFAD2', 'user', 3000.00, '{"laptop"}', '{}'),
    ('b0000000-0000-0000-0000-000000000004', 'userC', '$2a$10$pp.NmQ27Jz1aeJiA2fPJTu79LQhY9v/Pxh02nSJvQ5k1cpq7BFAD2', 'user', 4000.00, '{}', '{"Apple"}')
ON CONFLICT (username) DO NOTHING;

INSERT INTO products (id, category, manufacturer, model, price, stock_quantity)
VALUES
    ('c0000000-0000-0000-0000-000000000001', 'laptop', 'Apple', 'MacBook Pro 16 M3', 2499.00, 15),
    ('c0000000-0000-0000-0000-000000000002', 'laptop', 'Dell', 'XPS 15 9530', 1899.00, 20),
    ('c0000000-0000-0000-0000-000000000003', 'smartphone', 'Apple', 'iPhone 15 Pro Max', 1199.00, 30),
    ('c0000000-0000-0000-0000-000000000004', 'smartphone', 'Samsung', 'Galaxy S24 Ultra', 1299.00, 25),
    ('c0000000-0000-0000-0000-000000000005', 'smartphone', 'Xiaomi', 'Xiaomi 14 Ultra', 999.00, 18)
ON CONFLICT (id) DO NOTHING;
