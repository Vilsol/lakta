-- restaurants
CREATE TABLE IF NOT EXISTS restaurants
(
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(255) NOT NULL,
    cuisine_type VARCHAR(100),
    address      TEXT         NOT NULL,
    created_at   TIMESTAMP        DEFAULT NOW()
);

-- menu items
CREATE TABLE IF NOT EXISTS menu_items
(
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    restaurant_id UUID           NOT NULL REFERENCES restaurants (id) ON DELETE CASCADE,
    name          VARCHAR(255)   NOT NULL,
    price         DECIMAL(10, 2) NOT NULL,
    is_available  BOOLEAN          DEFAULT true,
    created_at    TIMESTAMP        DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_menu_items_restaurant ON menu_items (restaurant_id);

-- customers
CREATE TABLE IF NOT EXISTS customers
(
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      VARCHAR(255) UNIQUE NOT NULL,
    name       VARCHAR(255)        NOT NULL,
    created_at TIMESTAMP        DEFAULT NOW()
);

-- orders
CREATE TABLE IF NOT EXISTS orders
(
    id               UUID PRIMARY KEY        DEFAULT gen_random_uuid(),
    customer_id      UUID           NOT NULL REFERENCES customers (id),
    restaurant_id    UUID           NOT NULL REFERENCES restaurants (id),
    status           VARCHAR(50)    NOT NULL DEFAULT 'PLACED',
    delivery_address TEXT           NOT NULL,
    subtotal         BIGINT         NOT NULL,
    delivery_fee     BIGINT         NOT NULL,
    tax              BIGINT         NOT NULL,
    total            BIGINT         NOT NULL,
    created_at       TIMESTAMP               DEFAULT NOW(),
    updated_at       TIMESTAMP               DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_customer ON orders (customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders (status);

-- order items
CREATE TABLE IF NOT EXISTS order_items
(
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id     UUID           NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    menu_item_id UUID           NOT NULL REFERENCES menu_items (id),
    quantity     INT            NOT NULL,
    unit_price   DECIMAL(10, 2) NOT NULL,
    created_at   TIMESTAMP        DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_order_items_order ON order_items (order_id);