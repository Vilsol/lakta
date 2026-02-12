# Food Delivery Platform - Simplified Microservices Demo

## Overview

A minimal microservices demonstration for a food delivery platform using Lakta framework. The platform allows customers to browse restaurants, place orders, and have them delivered.

### Core Goals
- **HTTP REST API** for customer interactions
- **gRPC** for inter-service communication
- **PostgreSQL** for data persistence
- **Simple workflow** for order processing
- **Minimal business logic** to demonstrate patterns

---

## Architecture Design

```
┌──────────────┐
│   Customer   │
└──────┬───────┘
       │ HTTP/REST
       │
┌──────▼───────────────────────────┐
│        API Service               │
│  - HTTP REST endpoints           │
│  - Request validation            │
│  - Simple pricing                │
└──────┬───────────────┬───────────┘
       │ gRPC          │ gRPC
       │               │
┌──────▼──────────┐   ┌▼──────────────────┐
│  Data Service   │◄──┤ Workflow Service  │
│  - gRPC only    │   │ - Order processing│
│  - DB access    │   │ - Status updates  │
└──────┬──────────┘   └───────────────────┘
       │ SQL
┌──────▼──────────┐
│   PostgreSQL    │
└─────────────────┘
```

### Services

1. **Data Service** - Database access layer
2. **API Service** - Customer-facing HTTP API
3. **Workflow Service** - Order processing and lifecycle

---

## Service Specifications

### 1. Data Service

**Purpose**: Single source of truth for database operations.

**Protocol**: gRPC only

**Responsibilities**:
- Execute database queries
- No business logic

**gRPC Service**:

```protobuf
service DataService {
  // Restaurants
  rpc ListRestaurants(ListRestaurantsRequest) returns (RestaurantList);
  rpc GetRestaurant(GetRestaurantRequest) returns (Restaurant);
  rpc GetMenu(GetMenuRequest) returns (MenuItemList);

  // Orders
  rpc CreateOrder(CreateOrderRequest) returns (Order);
  rpc GetOrder(GetOrderRequest) returns (Order);
  rpc UpdateOrderStatus(UpdateOrderStatusRequest) returns (Order);
  rpc ListCustomerOrders(ListCustomerOrdersRequest) returns (OrderList);
}
```

---

### 2. API Service

**Purpose**: HTTP REST API for customers.

**Protocol**: HTTP REST

**Responsibilities**:
- Expose REST endpoints
- Request validation
- Simple pricing calculation (subtotal + fixed delivery fee)

**Endpoints**:

#### Restaurants
- `GET /restaurants` - List all restaurants
- `GET /restaurants/{id}` - Get restaurant details
- `GET /restaurants/{id}/menu` - Get menu

#### Orders
- `POST /orders` - Create order
  - Body: `{ customer_id, restaurant_id, items: [{menu_item_id, quantity}], delivery_address }`
  - Returns: Order with ID and total
- `GET /orders/{id}` - Get order details
- `GET /customers/{id}/orders` - Get customer's orders

**Simple Pricing**:
```
subtotal = sum(item.price * item.quantity)
delivery_fee = $5.00 (fixed)
tax = subtotal * 0.10
total = subtotal + delivery_fee + tax
```

---

### 3. Workflow Service

**Purpose**: Handle order lifecycle.

**Protocol**: gRPC

**Responsibilities**:
- Process order state transitions
- Simple workflow execution

**Order States**:
1. `PLACED` - Order created
2. `CONFIRMED` - Restaurant confirmed
3. `PREPARING` - Being prepared
4. `READY` - Ready for delivery
5. `DELIVERED` - Completed

**State Transitions**:
- PLACED → CONFIRMED (auto-confirm after 1 minute)
- CONFIRMED → PREPARING (auto-transition after 30 seconds)
- PREPARING → READY (auto-transition after 2 minutes)
- READY → DELIVERED (manual confirmation via API)

**gRPC Service**:
```protobuf
service WorkflowService {
  rpc StartOrderWorkflow(StartOrderWorkflowRequest) returns (Empty);
  rpc CompleteOrder(CompleteOrderRequest) returns (Order);
}
```

---

## Database Schema

### restaurants
```sql
CREATE TABLE restaurants (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  cuisine_type VARCHAR(100),
  address TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
```

### menu_items
```sql
CREATE TABLE menu_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id UUID NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  price DECIMAL(10, 2) NOT NULL,
  is_available BOOLEAN DEFAULT true,
  created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_menu_items_restaurant ON menu_items(restaurant_id);
```

### customers
```sql
CREATE TABLE customers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email VARCHAR(255) UNIQUE NOT NULL,
  name VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);
```

### orders
```sql
CREATE TABLE orders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id UUID NOT NULL REFERENCES customers(id),
  restaurant_id UUID NOT NULL REFERENCES restaurants(id),
  status VARCHAR(50) NOT NULL DEFAULT 'PLACED',
  delivery_address TEXT NOT NULL,
  subtotal DECIMAL(10, 2) NOT NULL,
  delivery_fee DECIMAL(10, 2) NOT NULL,
  tax DECIMAL(10, 2) NOT NULL,
  total DECIMAL(10, 2) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_status ON orders(status);
```

### order_items
```sql
CREATE TABLE order_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  menu_item_id UUID NOT NULL REFERENCES menu_items(id),
  quantity INT NOT NULL,
  unit_price DECIMAL(10, 2) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_order_items_order ON order_items(order_id);
```

---

## Example API Responses

### GET /restaurants
```json
{
  "restaurants": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Pizza Palace",
      "cuisine_type": "Italian",
      "address": "123 Main St"
    }
  ]
}
```

### POST /orders
Request:
```json
{
  "customer_id": "770e8400-e29b-41d4-a716-446655440000",
  "restaurant_id": "550e8400-e29b-41d4-a716-446655440000",
  "items": [
    {
      "menu_item_id": "660e8400-e29b-41d4-a716-446655440001",
      "quantity": 2
    }
  ],
  "delivery_address": "456 Oak Ave"
}
```

Response:
```json
{
  "order_id": "880e8400-e29b-41d4-a716-446655440000",
  "status": "PLACED",
  "subtotal": 25.98,
  "delivery_fee": 5.00,
  "tax": 2.60,
  "total": 33.58,
  "created_at": "2025-01-18T18:30:00Z"
}
```

### GET /orders/{id}
```json
{
  "id": "880e8400-e29b-41d4-a716-446655440000",
  "status": "PREPARING",
  "customer": {
    "id": "770e8400-e29b-41d4-a716-446655440000",
    "name": "John Doe"
  },
  "restaurant": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Pizza Palace"
  },
  "items": [
    {
      "name": "Margherita Pizza",
      "quantity": 2,
      "unit_price": 12.99
    }
  ],
  "delivery_address": "456 Oak Ave",
  "subtotal": 25.98,
  "delivery_fee": 5.00,
  "tax": 2.60,
  "total": 33.58,
  "created_at": "2025-01-18T18:30:00Z"
}
```

---

## Implementation Flow

### 1. Customer Places Order

```
Customer → API Service:
  POST /orders

API Service → Data Service:
  1. GetMenu(restaurant_id) - verify items exist
  2. CreateOrder(order_data) - save to DB

API Service → Workflow Service:
  StartOrderWorkflow(order_id)

API Service → Customer:
  Return order confirmation

Workflow Service:
  (background) Auto-transition through states
```

### 2. Order Workflow

```
Workflow Service starts workflow:

1. Wait 1 minute → Update status to CONFIRMED
2. Wait 30 seconds → Update status to PREPARING
3. Wait 2 minutes → Update status to READY
4. Wait for CompleteOrder() call → Update status to DELIVERED
```

Each status update calls:
```
Data Service.UpdateOrderStatus(order_id, new_status)
```

---

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22+ |
| HTTP Framework | Fiber v3 |
| gRPC | grpc-go |
| Database | PostgreSQL 16 |
| Database Driver | pgx v5 |
| Workflow | Simple goroutine-based state machine |
| Query Builder | squirrel |

---

## Development Setup

### 1. Database
```bash
docker run -d \
  --name postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16
```

### 2. Run Services
```bash
# Data service
go run ./data/main.go

# API service
go run ./api/main.go

# Workflow service
go run ./workflow/main.go
```

---

## Testing Flow

### 1. Seed Database
```bash
# Insert test restaurant and menu items
psql -U postgres -f seed.sql
```

### 2. Create Order
```bash
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "...",
    "restaurant_id": "...",
    "items": [{"menu_item_id": "...", "quantity": 2}],
    "delivery_address": "123 Test St"
  }'
```

### 3. Check Status
```bash
curl http://localhost:8080/orders/{order_id}
```

Status will auto-progress: PLACED → CONFIRMED → PREPARING → READY

### 4. Complete Delivery
```bash
curl -X POST http://localhost:8080/orders/{order_id}/complete
```

Status changes to DELIVERED

---

## Key Simplifications from Full Version

**Removed**:
- Driver management and assignment
- Real-time location tracking
- Surge pricing and dynamic fees
- Complex matching algorithms
- Retry and compensation logic
- Geographic queries and delivery radius
- Restaurant operating hours
- Payment integration
- Authentication/authorization
- Multiple timeout scenarios
- Advanced workflow engine (Temporal)

**Kept**:
- 3-service architecture
- gRPC for service-to-service
- HTTP REST for customers
- PostgreSQL for persistence
- Basic order lifecycle
- Simple state transitions

---

## Project Structure

```
microservices/
├── proto/
│   ├── data.proto
│   └── workflow.proto
├── data/
│   ├── main.go
│   ├── module.go
│   └── service.go
├── api/
│   ├── main.go
│   ├── module.go
│   └── handlers.go
├── workflow/
│   ├── main.go
│   ├── module.go
│   └── engine.go
├── migrations/
│   └── 001_initial.sql
└── README.md
```

---

## Future Enhancements

If you want to expand the demo:
1. Add driver management (simple assignment)
2. Add basic authentication
3. Add email notifications
4. Add order cancellation
5. Add restaurant admin panel

---

**Document Version**: 1.0-simplified
**Status**: Ready for Implementation