# Food Delivery Platform - Microservices Architecture

## Table of Contents
1. [Overview](#overview)
2. [Architecture Design](#architecture-design)
3. [Service Specifications](#service-specifications)
4. [Database Schema](#database-schema)
5. [API Documentation](#api-documentation)
6. [Workflow Specifications](#workflow-specifications)
7. [Data Flow Examples](#data-flow-examples)
8. [Technology Stack](#technology-stack)

---

## Overview

This document describes a microservices-based food delivery platform designed to demonstrate a modern distributed system architecture. The platform enables customers to browse restaurants, place orders, and track deliveries in real-time.

### Key Requirements
- **HTTP REST API** for customer-facing interactions
- **gRPC** for inter-service communication
- **PostgreSQL** for data persistence
- **Workflow orchestration** for order lifecycle management
- **Custom business logic** for pricing, matching, and validation

### Design Principles
- **Separation of Concerns**: Data access, business logic, and API layers are isolated
- **Single Database Owner**: Only the Data Service communicates with PostgreSQL
- **Service Independence**: Each service can be developed, deployed, and scaled independently
- **Clear Contracts**: Well-defined gRPC and HTTP APIs

---

## Architecture Design

### High-Level Architecture

```
┌──────────────┐
│   Customer   │
└──────┬───────┘
       │ HTTP/REST
       │
┌──────▼───────────────────────────────────────────────────┐
│                    API Service                           │
│  - HTTP REST endpoints                                   │
│  - Request validation                                    │
│  - Business logic: pricing, availability, ETA            │
└──────┬───────────────────────────────────────┬───────────┘
       │ gRPC                                  │ gRPC
       │                                       │
┌──────▼──────────────────┐         ┌─────────▼─────────────┐
│   Data Service          │◄────────┤  Orchestration Service│
│   - gRPC only           │  gRPC   │  - Workflow engine    │
│   - Database access     │         │  - Driver matching    │
│   - CRUD operations     │         │  - Event handling     │
└──────┬──────────────────┘         └───────────────────────┘
       │
       │ SQL
┌──────▼──────────────────┐
│     PostgreSQL          │
│  - Restaurants          │
│  - Orders               │
│  - Drivers              │
│  - Customers            │
└─────────────────────────┘
```

### Service Count: 3

1. **Data Service** - Database abstraction layer
2. **API Service** - Customer-facing HTTP API
3. **Orchestration Service** - Workflow and backend processing

---

## Service Specifications

### 1. Data Service

**Purpose**: Single source of truth for all database operations. Acts as a data access layer that abstracts PostgreSQL from other services.

**Protocol**: gRPC only (internal service)

**Responsibilities**:
- Execute all database queries (SELECT, INSERT, UPDATE, DELETE)
- Manage database connections and pooling
- Enforce data integrity constraints
- No business logic (pure data access)

**gRPC Service Definition**:

```protobuf
service DataService {
  // Restaurant operations
  rpc GetRestaurant(GetRestaurantRequest) returns (Restaurant);
  rpc SearchRestaurants(SearchRestaurantsRequest) returns (RestaurantList);
  rpc GetMenu(GetMenuRequest) returns (MenuItemList);

  // Order operations
  rpc CreateOrder(CreateOrderRequest) returns (Order);
  rpc GetOrder(GetOrderRequest) returns (Order);
  rpc UpdateOrderStatus(UpdateOrderStatusRequest) returns (Order);
  rpc GetOrderHistory(GetOrderHistoryRequest) returns (OrderList);
  rpc AssignDriverToOrder(AssignDriverRequest) returns (Order);

  // Driver operations
  rpc FindAvailableDrivers(FindDriversRequest) returns (DriverList);
  rpc GetDriver(GetDriverRequest) returns (Driver);
  rpc UpdateDriverLocation(UpdateLocationRequest) returns (Driver);
  rpc GetDriverLocation(GetDriverLocationRequest) returns (Location);
  rpc UpdateDriverStatus(UpdateDriverStatusRequest) returns (Driver);

  // Customer operations
  rpc GetCustomer(GetCustomerRequest) returns (Customer);
  rpc CreateCustomer(CreateCustomerRequest) returns (Customer);
}
```

**Dependencies**:
- PostgreSQL database connection

**Scaling Characteristics**:
- Stateless (can horizontally scale)
- Read replicas can be used for read-heavy operations
- Connection pooling for efficient database access

---

### 2. API Service

**Purpose**: Customer-facing HTTP REST API that provides all public endpoints for the platform.

**Protocol**: HTTP REST (public)

**Responsibilities**:
- Expose RESTful endpoints for client applications
- Request/response validation
- Authentication and authorization
- Business logic:
  - **Dynamic pricing calculation** (delivery fees based on distance and demand)
  - **Restaurant availability checks** (operating hours, delivery radius)
  - **Order validation** (menu items exist, minimum order amount)
  - **ETA calculations** (distance-based delivery estimates)

**HTTP Endpoints**:

#### Restaurants
- `GET /restaurants`
  - Query params: `latitude`, `longitude`, `cuisine_type`, `min_rating`
  - Returns: List of restaurants

- `GET /restaurants/{id}`
  - Returns: Restaurant details (name, address, rating, cuisine, operating hours)

- `GET /restaurants/{id}/menu`
  - Returns: Menu items with prices and availability

#### Orders
- `POST /orders`
  - Body: `{ customer_id, restaurant_id, items: [{menu_item_id, quantity}], delivery_address }`
  - Returns: Order confirmation with order_id and estimated delivery time

- `GET /orders/{id}`
  - Returns: Order details (status, items, driver info, tracking)

- `GET /orders/{id}/driver-location`
  - Returns: Real-time driver coordinates and ETA

#### Customer
- `GET /customers/{id}/orders`
  - Query params: `status`, `limit`, `offset`
  - Returns: Customer's order history

**Business Logic Details**:

1. **Dynamic Pricing**:
   ```
   base_delivery_fee = 2.99
   distance_fee = distance_km * 0.50
   surge_multiplier = calculate_surge(zone, time)
   total_delivery_fee = (base_delivery_fee + distance_fee) * surge_multiplier
   ```

2. **Availability Check**:
   - Verify current time is within restaurant operating hours
   - Calculate distance between restaurant and delivery address
   - Check if distance <= restaurant's delivery radius
   - Verify all menu items are in stock

3. **ETA Calculation**:
   ```
   preparation_time = 20 minutes (average)
   delivery_time = (distance_km / average_speed_kmh) * 60
   eta_minutes = preparation_time + delivery_time + buffer(5 minutes)
   ```

**Dependencies**:
- Data Service (gRPC client)
- Orchestration Service (async event trigger)

---

### 3. Orchestration Service

**Purpose**: Handles complex workflows, background jobs, and business logic that requires coordination across multiple operations.

**Protocol**: gRPC (internal service-to-service)

**Responsibilities**:
- Execute order lifecycle workflows
- Driver assignment and matching
- Event processing and notifications
- Background jobs (cleanup, analytics)
- Business logic:
  - **Driver matching algorithm** (proximity, rating, acceptance rate)
  - **Surge pricing calculations** (demand monitoring)
  - **Timeout and retry handling**
  - **Compensation logic** (cancellations, refunds)

**Workflow Definitions**:

#### Order Lifecycle Workflow

```
State Machine:
  PLACED → RESTAURANT_CONFIRMED → PREPARING → READY →
  DRIVER_ASSIGNED → PICKED_UP → DELIVERING → DELIVERED

Transitions:
  1. PLACED (order created by customer)
     - Trigger: API Service creates order
     - Actions:
       - Wait for restaurant confirmation (timeout: 5 minutes)
       - If timeout: auto-cancel order

  2. RESTAURANT_CONFIRMED
     - Trigger: Restaurant accepts order
     - Actions:
       - Notify customer
       - Start preparation timer

  3. PREPARING
     - Trigger: Restaurant marks as preparing
     - Actions:
       - Update ETA

  4. READY
     - Trigger: Food is ready for pickup
     - Actions:
       - Find available drivers
       - Execute driver matching algorithm
       - Assign best driver

  5. DRIVER_ASSIGNED
     - Trigger: Driver accepts assignment
     - Actions:
       - Notify customer with driver details
       - Start location tracking

  6. PICKED_UP
     - Trigger: Driver confirms pickup
     - Actions:
       - Update order status
       - Enable real-time tracking

  7. DELIVERING
     - Trigger: Driver en route
     - Actions:
       - Stream location updates
       - Calculate dynamic ETA

  8. DELIVERED
     - Trigger: Driver confirms delivery
     - Actions:
       - Complete order
       - Request customer rating
       - Process payment settlement
```

**Business Logic Details**:

1. **Driver Matching Algorithm**:
   ```
   Input: order_id, delivery_location

   Steps:
   1. Get drivers within 5km radius (Data Service)
   2. Filter: status == AVAILABLE && on_duty == true
   3. Calculate score for each driver:
      proximity_score = (1 - distance/max_distance) * 0.6
      rating_score = driver_rating / 5.0 * 0.3
      acceptance_score = acceptance_rate * 0.1
      total_score = proximity_score + rating_score + acceptance_score
   4. Sort drivers by total_score (descending)
   5. Assign to top driver
   6. If rejected, try next driver (max 3 attempts)
   7. If all reject, increase delivery fee and retry
   ```

2. **Surge Pricing Calculation**:
   ```
   Monitor active orders per zone:
   - If active_orders < 10: surge_multiplier = 1.0
   - If 10 <= active_orders < 20: surge_multiplier = 1.25
   - If 20 <= active_orders < 30: surge_multiplier = 1.5
   - If active_orders >= 30: surge_multiplier = 2.0

   Time-based adjustments:
   - Lunch (11am-2pm): +0.2 multiplier
   - Dinner (6pm-9pm): +0.3 multiplier
   - Late night (10pm-2am): +0.4 multiplier
   ```

3. **Retry Policy**:
   - Restaurant confirmation: 3 retries with 1-minute intervals
   - Driver assignment: 3 attempts per driver, up to 5 drivers
   - Payment processing: Exponential backoff (1s, 2s, 4s, 8s)

4. **Compensation Logic**:
   ```
   Cancellation scenarios:
   - Restaurant doesn't confirm (5 min): Full refund + 10% credit
   - No drivers available (10 min): Full refund + 15% credit
   - Customer cancels after PREPARING: Charge cancellation fee (30%)
   - Restaurant cancels after CONFIRMED: Full refund + 20% credit
   ```

**gRPC Service Definition**:

```protobuf
service OrchestrationService {
  rpc StartOrderWorkflow(StartOrderWorkflowRequest) returns (WorkflowResponse);
  rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
  rpc HandleDriverResponse(DriverResponseRequest) returns (Empty);
  rpc CalculateSurgeMultiplier(SurgeRequest) returns (SurgeResponse);
}
```

**Dependencies**:
- Data Service (gRPC client)
- Message queue/event bus (for async processing)

---

## Database Schema

### PostgreSQL Tables

#### restaurants
```sql
CREATE TABLE restaurants (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  cuisine_type VARCHAR(100),
  address TEXT NOT NULL,
  latitude DECIMAL(10, 8) NOT NULL,
  longitude DECIMAL(11, 8) NOT NULL,
  delivery_radius_km DECIMAL(5, 2) DEFAULT 5.0,
  min_order_amount DECIMAL(10, 2) DEFAULT 0,
  rating DECIMAL(3, 2) DEFAULT 0,
  is_open BOOLEAN DEFAULT true,
  operating_hours JSONB,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_restaurants_location ON restaurants USING GIST(
  ll_to_earth(latitude, longitude)
);
```

#### menu_items
```sql
CREATE TABLE menu_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  restaurant_id UUID NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  price DECIMAL(10, 2) NOT NULL,
  category VARCHAR(100),
  is_available BOOLEAN DEFAULT true,
  image_url TEXT,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_menu_items_restaurant ON menu_items(restaurant_id);
```

#### customers
```sql
CREATE TABLE customers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email VARCHAR(255) UNIQUE NOT NULL,
  phone VARCHAR(20),
  name VARCHAR(255) NOT NULL,
  default_address TEXT,
  default_latitude DECIMAL(10, 8),
  default_longitude DECIMAL(11, 8),
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);
```

#### drivers
```sql
CREATE TABLE drivers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  phone VARCHAR(20) NOT NULL,
  vehicle_type VARCHAR(50),
  license_plate VARCHAR(20),
  rating DECIMAL(3, 2) DEFAULT 5.0,
  total_deliveries INT DEFAULT 0,
  acceptance_rate DECIMAL(3, 2) DEFAULT 1.0,
  status VARCHAR(20) DEFAULT 'OFFLINE', -- OFFLINE, AVAILABLE, BUSY
  current_latitude DECIMAL(10, 8),
  current_longitude DECIMAL(11, 8),
  last_location_update TIMESTAMP,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_drivers_location ON drivers USING GIST(
  ll_to_earth(current_latitude, current_longitude)
);
CREATE INDEX idx_drivers_status ON drivers(status);
```

#### orders
```sql
CREATE TABLE orders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id UUID NOT NULL REFERENCES customers(id),
  restaurant_id UUID NOT NULL REFERENCES restaurants(id),
  driver_id UUID REFERENCES drivers(id),
  status VARCHAR(50) NOT NULL DEFAULT 'PLACED',
  delivery_address TEXT NOT NULL,
  delivery_latitude DECIMAL(10, 8) NOT NULL,
  delivery_longitude DECIMAL(11, 8) NOT NULL,
  subtotal DECIMAL(10, 2) NOT NULL,
  delivery_fee DECIMAL(10, 2) NOT NULL,
  tax DECIMAL(10, 2) NOT NULL,
  total DECIMAL(10, 2) NOT NULL,
  estimated_delivery_time TIMESTAMP,
  actual_delivery_time TIMESTAMP,
  special_instructions TEXT,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_restaurant ON orders(restaurant_id);
CREATE INDEX idx_orders_driver ON orders(driver_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at DESC);
```

#### order_items
```sql
CREATE TABLE order_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  menu_item_id UUID NOT NULL REFERENCES menu_items(id),
  quantity INT NOT NULL,
  unit_price DECIMAL(10, 2) NOT NULL,
  subtotal DECIMAL(10, 2) NOT NULL,
  special_requests TEXT,
  created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_order_items_order ON order_items(order_id);
```

#### order_status_history
```sql
CREATE TABLE order_status_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  status VARCHAR(50) NOT NULL,
  notes TEXT,
  created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_order_status_history_order ON order_status_history(order_id);
```

#### driver_locations
```sql
CREATE TABLE driver_locations (
  id BIGSERIAL PRIMARY KEY,
  driver_id UUID NOT NULL REFERENCES drivers(id) ON DELETE CASCADE,
  latitude DECIMAL(10, 8) NOT NULL,
  longitude DECIMAL(11, 8) NOT NULL,
  speed_kmh DECIMAL(5, 2),
  heading_degrees INT,
  recorded_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_driver_locations_driver ON driver_locations(driver_id, recorded_at DESC);
```

---

## API Documentation

### REST API (API Service)

#### Base URL
```
http://api.example.com/v1
```

#### Authentication
```
Authorization: Bearer <jwt_token>
```

---

### Restaurants API

#### GET /restaurants
Search for restaurants based on location and filters.

**Query Parameters**:
- `latitude` (required): Decimal, customer's latitude
- `longitude` (required): Decimal, customer's longitude
- `cuisine_type` (optional): String, filter by cuisine
- `min_rating` (optional): Decimal, minimum restaurant rating
- `limit` (optional): Integer, default 20
- `offset` (optional): Integer, default 0

**Response** (200 OK):
```json
{
  "restaurants": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "Pizza Palace",
      "cuisine_type": "Italian",
      "rating": 4.5,
      "delivery_time_minutes": 30,
      "delivery_fee": 3.99,
      "min_order_amount": 10.00,
      "is_open": true,
      "distance_km": 2.3
    }
  ],
  "total": 15,
  "limit": 20,
  "offset": 0
}
```

---

#### GET /restaurants/{id}
Get detailed information about a specific restaurant.

**Path Parameters**:
- `id`: UUID of the restaurant

**Response** (200 OK):
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Pizza Palace",
  "cuisine_type": "Italian",
  "address": "123 Main St, City",
  "latitude": 40.7128,
  "longitude": -74.0060,
  "rating": 4.5,
  "delivery_radius_km": 5.0,
  "min_order_amount": 10.00,
  "is_open": true,
  "operating_hours": {
    "monday": "11:00-22:00",
    "tuesday": "11:00-22:00",
    "wednesday": "11:00-22:00",
    "thursday": "11:00-23:00",
    "friday": "11:00-23:00",
    "saturday": "12:00-23:00",
    "sunday": "12:00-21:00"
  }
}
```

---

#### GET /restaurants/{id}/menu
Get the menu for a specific restaurant.

**Path Parameters**:
- `id`: UUID of the restaurant

**Response** (200 OK):
```json
{
  "restaurant_id": "550e8400-e29b-41d4-a716-446655440000",
  "categories": [
    {
      "name": "Pizzas",
      "items": [
        {
          "id": "660e8400-e29b-41d4-a716-446655440001",
          "name": "Margherita",
          "description": "Classic tomato and mozzarella",
          "price": 12.99,
          "is_available": true,
          "image_url": "https://example.com/margherita.jpg"
        }
      ]
    }
  ]
}
```

---

### Orders API

#### POST /orders
Create a new order.

**Request Body**:
```json
{
  "customer_id": "770e8400-e29b-41d4-a716-446655440000",
  "restaurant_id": "550e8400-e29b-41d4-a716-446655440000",
  "items": [
    {
      "menu_item_id": "660e8400-e29b-41d4-a716-446655440001",
      "quantity": 2,
      "special_requests": "Extra cheese"
    }
  ],
  "delivery_address": "456 Oak Ave, City",
  "delivery_latitude": 40.7589,
  "delivery_longitude": -73.9851,
  "special_instructions": "Ring doorbell"
}
```

**Response** (201 Created):
```json
{
  "order_id": "880e8400-e29b-41d4-a716-446655440000",
  "status": "PLACED",
  "subtotal": 25.98,
  "delivery_fee": 4.49,
  "tax": 2.60,
  "total": 33.07,
  "estimated_delivery_time": "2025-01-18T19:30:00Z",
  "created_at": "2025-01-18T18:30:00Z"
}
```

**Error Response** (400 Bad Request):
```json
{
  "error": "VALIDATION_ERROR",
  "message": "Restaurant is outside delivery radius",
  "details": {
    "distance_km": 7.2,
    "max_delivery_radius_km": 5.0
  }
}
```

---

#### GET /orders/{id}
Get order details and current status.

**Path Parameters**:
- `id`: UUID of the order

**Response** (200 OK):
```json
{
  "id": "880e8400-e29b-41d4-a716-446655440000",
  "status": "DRIVER_ASSIGNED",
  "customer": {
    "id": "770e8400-e29b-41d4-a716-446655440000",
    "name": "John Doe"
  },
  "restaurant": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Pizza Palace",
    "phone": "555-1234"
  },
  "driver": {
    "id": "990e8400-e29b-41d4-a716-446655440000",
    "name": "Jane Smith",
    "phone": "555-5678",
    "vehicle_type": "Motorcycle",
    "rating": 4.8
  },
  "items": [
    {
      "name": "Margherita",
      "quantity": 2,
      "unit_price": 12.99,
      "subtotal": 25.98
    }
  ],
  "delivery_address": "456 Oak Ave, City",
  "subtotal": 25.98,
  "delivery_fee": 4.49,
  "tax": 2.60,
  "total": 33.07,
  "estimated_delivery_time": "2025-01-18T19:30:00Z",
  "status_history": [
    {
      "status": "PLACED",
      "timestamp": "2025-01-18T18:30:00Z"
    },
    {
      "status": "RESTAURANT_CONFIRMED",
      "timestamp": "2025-01-18T18:32:00Z"
    },
    {
      "status": "DRIVER_ASSIGNED",
      "timestamp": "2025-01-18T18:45:00Z"
    }
  ],
  "created_at": "2025-01-18T18:30:00Z"
}
```

---

#### GET /orders/{id}/driver-location
Get real-time driver location for active delivery.

**Path Parameters**:
- `id`: UUID of the order

**Response** (200 OK):
```json
{
  "order_id": "880e8400-e29b-41d4-a716-446655440000",
  "driver_id": "990e8400-e29b-41d4-a716-446655440000",
  "current_location": {
    "latitude": 40.7500,
    "longitude": -73.9900,
    "updated_at": "2025-01-18T19:15:23Z"
  },
  "eta_minutes": 12,
  "distance_remaining_km": 1.8
}
```

---

#### GET /customers/{id}/orders
Get customer's order history.

**Path Parameters**:
- `id`: UUID of the customer

**Query Parameters**:
- `status` (optional): Filter by status
- `limit` (optional): Integer, default 20
- `offset` (optional): Integer, default 0

**Response** (200 OK):
```json
{
  "orders": [
    {
      "id": "880e8400-e29b-41d4-a716-446655440000",
      "restaurant_name": "Pizza Palace",
      "status": "DELIVERED",
      "total": 33.07,
      "created_at": "2025-01-18T18:30:00Z",
      "delivered_at": "2025-01-18T19:28:00Z"
    }
  ],
  "total": 25,
  "limit": 20,
  "offset": 0
}
```

---

## Workflow Specifications

### Order Lifecycle Workflow

**Workflow Engine**: Temporal / Cadence / Custom State Machine

**Workflow ID**: `order-lifecycle-{order_id}`

**States**:
1. `PLACED`
2. `RESTAURANT_CONFIRMED`
3. `PREPARING`
4. `READY`
5. `DRIVER_ASSIGNED`
6. `PICKED_UP`
7. `DELIVERING`
8. `DELIVERED`

**Terminal States**:
- `CANCELLED`
- `FAILED`

---

### State Transition Details

#### 1. PLACED → RESTAURANT_CONFIRMED

**Trigger**: Order created via API Service

**Activities**:
1. Call Data Service: `CreateOrder()`
2. Send notification to restaurant
3. Start timeout timer (5 minutes)
4. Wait for restaurant confirmation or timeout

**Timeout Behavior**:
- After 5 minutes without confirmation: Transition to `CANCELLED`
- Execute compensation: Refund customer + 10% credit
- Send notification to customer

**Success Condition**: Restaurant confirms order

---

#### 2. RESTAURANT_CONFIRMED → PREPARING

**Trigger**: Restaurant accepts order

**Activities**:
1. Call Data Service: `UpdateOrderStatus(order_id, "PREPARING")`
2. Send notification to customer: "Your order is being prepared"
3. Calculate preparation time estimate
4. Update ETA

---

#### 3. PREPARING → READY

**Trigger**: Restaurant marks food as ready

**Activities**:
1. Call Data Service: `UpdateOrderStatus(order_id, "READY")`
2. Initiate driver assignment process
3. Call Data Service: `FindAvailableDrivers(location, radius)`
4. Execute driver matching algorithm
5. Send assignment request to top driver

---

#### 4. READY → DRIVER_ASSIGNED

**Trigger**: Driver accepts assignment

**Activities**:
1. Call Data Service: `AssignDriverToOrder(order_id, driver_id)`
2. Call Data Service: `UpdateOrderStatus(order_id, "DRIVER_ASSIGNED")`
3. Send notification to customer with driver details
4. Start location tracking
5. Calculate ETA

**Retry Logic**:
- If driver rejects: Try next driver (max 3 drivers)
- If all drivers reject: Increase delivery fee by 20% and retry
- If no drivers available after 10 minutes: Cancel order with compensation

---

#### 5. DRIVER_ASSIGNED → PICKED_UP

**Trigger**: Driver confirms pickup at restaurant

**Activities**:
1. Call Data Service: `UpdateOrderStatus(order_id, "PICKED_UP")`
2. Start delivery tracking
3. Enable real-time location streaming
4. Send notification to customer: "Your order is on the way"

---

#### 6. PICKED_UP → DELIVERING

**Trigger**: Driver marks as in transit

**Activities**:
1. Call Data Service: `UpdateOrderStatus(order_id, "DELIVERING")`
2. Stream location updates every 10 seconds
3. Recalculate ETA every 30 seconds
4. Monitor deviation from route (alert if >500m off course)

---

#### 7. DELIVERING → DELIVERED

**Trigger**: Driver confirms delivery

**Activities**:
1. Call Data Service: `UpdateOrderStatus(order_id, "DELIVERED")`
2. Record actual delivery time
3. Stop location tracking
4. Send notification to customer: "Your order has been delivered"
5. Request rating and feedback
6. Process payment settlement
7. Update driver statistics (total_deliveries++)

---

### Parallel Workflows

#### Driver Location Tracking Workflow

**Trigger**: Order status changes to `DRIVER_ASSIGNED`

**Activities**:
1. Subscribe to driver location updates
2. Every 10 seconds:
   - Call Data Service: `GetDriverLocation(driver_id)`
   - Store location in time-series database
   - Calculate distance to destination
   - Update ETA
3. Terminate when order status is `DELIVERED` or `CANCELLED`

---

#### Surge Pricing Monitoring Workflow

**Schedule**: Runs continuously

**Activities**:
1. Every 1 minute:
   - Query active orders per geographic zone
   - Calculate demand levels
   - Update surge multipliers
   - Store in cache (Redis)
2. API Service reads multiplier from cache when calculating delivery fees

---

## Data Flow Examples

### Example 1: Customer Places Order

```
┌─────────┐
│Customer │
└────┬────┘
     │
     │ POST /orders
     ▼
┌────────────────┐
│  API Service   │
└────┬───────┬───┘
     │       │
     │       │ (async event)
     │       ▼
     │  ┌──────────────────────┐
     │  │ Orchestration Service│
     │  └─────────┬────────────┘
     │            │
     │            │ Start workflow
     │            │
     ▼            ▼
┌────────────────────────┐
│    Data Service        │
└───────┬────────────────┘
        │
        ▼
┌────────────────┐
│   PostgreSQL   │
└────────────────┘

Detailed Steps:

1. Customer sends POST /orders request to API Service

2. API Service validates request:
   - Check customer exists
   - Verify restaurant is open
   - Validate menu items exist and are available
   - Calculate distance between restaurant and delivery address
   - Check if within delivery radius

3. API Service calculates pricing:
   - Call Data Service: GetRestaurant(restaurant_id)
   - Calculate subtotal from item prices
   - Calculate delivery fee: base_fee + distance_fee
   - Apply surge multiplier
   - Calculate tax
   - Calculate total

4. API Service creates order:
   - Call Data Service: CreateOrder(order_data)
   - Data Service inserts into orders table
   - Data Service inserts into order_items table
   - Returns order object

5. API Service triggers workflow:
   - Publish event: OrderCreated(order_id)
   - Return response to customer immediately

6. Orchestration Service receives event:
   - Start OrderLifecycleWorkflow(order_id)
   - Send notification to restaurant
   - Start 5-minute confirmation timer
```

---

### Example 2: Driver Assignment

```
┌──────────────────────┐
│ Orchestration Service│
│  (Workflow Running)  │
└──────────┬───────────┘
           │
           │ Order status: READY
           │
           ▼
      ┌─────────────────────────┐
      │ Execute Driver Matching │
      └────┬───────────┬────────┘
           │           │
           │           │ gRPC: FindAvailableDrivers()
           ▼           ▼
      ┌────────────────────┐
      │   Data Service     │
      └────┬───────────────┘
           │
           │ Query: SELECT * FROM drivers
           │ WHERE status='AVAILABLE'
           │ AND earth_distance(...) < 5000
           ▼
      ┌─────────────┐
      │ PostgreSQL  │
      └─────┬───────┘
            │
            │ Returns 8 available drivers
            ▼
      ┌──────────────────────┐
      │ Orchestration Service│
      └──────────┬───────────┘
                 │
                 │ Calculate scores:
                 │ Driver A: 0.85 (2km, 4.9★, 95%)
                 │ Driver B: 0.78 (3km, 4.7★, 92%)
                 │ Driver C: 0.72 (1.5km, 4.2★, 88%)
                 │
                 │ Sort by score
                 │ Select Driver A
                 │
                 ▼
            Send assignment request to Driver A
                 │
                 ▼
            ┌─────────────┐
            │  Driver A   │
            │   accepts   │
            └──────┬──────┘
                   │
                   ▼
      ┌──────────────────────┐
      │ Orchestration Service│
      └──────────┬───────────┘
                 │
                 │ gRPC: AssignDriverToOrder()
                 ▼
      ┌────────────────────┐
      │   Data Service     │
      └────┬───────────────┘
           │
           │ UPDATE orders SET driver_id=...
           ▼
      ┌─────────────┐
      │ PostgreSQL  │
      └─────────────┘
```

---

### Example 3: Real-Time Location Tracking

```
┌─────────┐
│ Customer│
└────┬────┘
     │
     │ GET /orders/{id}/driver-location
     ▼
┌────────────────┐
│  API Service   │
└────┬───────────┘
     │
     │ gRPC: GetDriverLocation(driver_id)
     ▼
┌────────────────────┐
│   Data Service     │
└────┬───────────────┘
     │
     │ SELECT * FROM drivers
     │ WHERE id = driver_id
     ▼
┌─────────────┐
│ PostgreSQL  │
└─────┬───────┘
      │
      │ Returns: current_latitude, current_longitude
      ▼
┌────────────────┐
│  API Service   │
└────┬───────────┘
     │
     │ Calculate:
     │ - Distance remaining to delivery address
     │ - ETA based on current speed
     ▼
┌─────────┐
│Customer │
│ (JSON)  │
└─────────┘


Parallel process (Driver's app):

┌──────────────┐
│  Driver App  │
└──────┬───────┘
       │
       │ Every 10 seconds
       │ POST location update
       ▼
┌────────────────────┐
│   Data Service     │
│ UpdateDriverLocation
└────┬───────────────┘
     │
     │ UPDATE drivers SET
     │ current_latitude=...,
     │ current_longitude=...,
     │ last_location_update=NOW()
     │
     │ INSERT INTO driver_locations
     ▼
┌─────────────┐
│ PostgreSQL  │
└─────────────┘
```

---

## Technology Stack

### Required Technologies

| Component | Technology Options | Recommendation |
|-----------|-------------------|----------------|
| **Programming Language** | Go, Java, Node.js, Python | Go (performance + native gRPC support) |
| **HTTP Framework** | Gin (Go), Express (Node), FastAPI (Python) | Gin |
| **gRPC Framework** | grpc-go, grpc-java, grpc-node | grpc-go |
| **Database** | PostgreSQL 15+ | PostgreSQL 16 |
| **Workflow Engine** | Temporal, Cadence, Custom | Temporal |
| **Message Queue** | RabbitMQ, Kafka, NATS | NATS |
| **Caching** | Redis, Memcached | Redis |
| **API Gateway** | Kong, Envoy, Traefik | Traefik |
| **Service Discovery** | Consul, etcd, Kubernetes DNS | Kubernetes DNS |
| **Monitoring** | Prometheus + Grafana | Prometheus + Grafana |
| **Logging** | ELK Stack, Loki | Loki + Grafana |
| **Tracing** | Jaeger, Zipkin | Jaeger |

---

### Service-Specific Stack

#### Data Service
- **Runtime**: Go 1.22+
- **Framework**: grpc-go
- **Database Driver**: pgx (PostgreSQL driver)
- **Connection Pooling**: pgxpool
- **Migrations**: golang-migrate
- **Code Generation**: protoc + protoc-gen-go

#### API Service
- **Runtime**: Go 1.22+
- **HTTP Framework**: Gin
- **gRPC Client**: grpc-go
- **Validation**: go-playground/validator
- **JWT Auth**: golang-jwt
- **Rate Limiting**: tollbooth

#### Orchestration Service
- **Runtime**: Go 1.22+
- **Workflow**: Temporal SDK
- **gRPC Client**: grpc-go
- **Event Bus**: NATS
- **Cron Jobs**: Temporal schedules

---

### Infrastructure

#### Development
```yaml
Docker Compose services:
  - postgres (PostgreSQL 16)
  - redis (Redis 7)
  - temporal (Temporal server + UI)
  - nats (NATS server)
```

#### Production (Kubernetes)
```yaml
Deployments:
  - data-service (3 replicas)
  - api-service (5 replicas with HPA)
  - orchestration-service (2 replicas)
  - temporal-workers (3 replicas)

StatefulSets:
  - postgres (with persistent volumes)
  - redis (with persistence)

Services:
  - data-service (ClusterIP)
  - api-service (LoadBalancer)
  - postgres (Headless)
```

---

### Development Tools

- **API Testing**: Postman, httpie
- **gRPC Testing**: grpcurl, BloomRPC
- **Database**: pgAdmin, TablePlus
- **Load Testing**: k6, vegeta
- **Code Quality**: golangci-lint

---

## Deployment Architecture

### Kubernetes Manifest Example

```yaml
# api-service deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-service
spec:
  replicas: 5
  selector:
    matchLabels:
      app: api-service
  template:
    metadata:
      labels:
        app: api-service
    spec:
      containers:
      - name: api-service
        image: food-delivery/api-service:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: DATA_SERVICE_ADDR
          value: "data-service:9090"
        - name: REDIS_ADDR
          value: "redis:6379"
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: api-service
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: api-service
```

---

## Security Considerations

### Authentication & Authorization
- **Customer Auth**: JWT tokens with 24h expiry
- **Service-to-Service**: mTLS for gRPC connections
- **API Keys**: For third-party integrations

### Data Protection
- **Encryption at Rest**: PostgreSQL encryption
- **Encryption in Transit**: TLS 1.3 for all connections
- **PII Handling**: Hash phone numbers, encrypt addresses

### Rate Limiting
- **API Service**: 100 requests/minute per customer
- **Order Creation**: 5 orders/hour per customer
- **Driver Location Updates**: 1 update/5 seconds

### Input Validation
- **SQL Injection**: Parameterized queries only
- **XSS**: Sanitize all user inputs
- **CSRF**: CSRF tokens for state-changing operations

---

## Monitoring & Observability

### Metrics (Prometheus)

#### API Service
- `http_requests_total` (counter)
- `http_request_duration_seconds` (histogram)
- `http_errors_total` (counter)
- `active_orders` (gauge)

#### Data Service
- `grpc_requests_total` (counter)
- `grpc_request_duration_seconds` (histogram)
- `db_connection_pool_size` (gauge)
- `db_query_duration_seconds` (histogram)

#### Orchestration Service
- `workflows_started_total` (counter)
- `workflows_completed_total` (counter)
- `workflows_failed_total` (counter)
- `driver_assignments_total` (counter)
- `driver_assignment_duration_seconds` (histogram)

### Logging

#### Structured Logging (JSON)
```json
{
  "timestamp": "2025-01-18T19:15:30Z",
  "level": "info",
  "service": "api-service",
  "trace_id": "abc123",
  "message": "Order created successfully",
  "order_id": "880e8400-e29b-41d4-a716-446655440000",
  "customer_id": "770e8400-e29b-41d4-a716-446655440000",
  "total": 33.07
}
```

### Tracing (Jaeger)
- Trace entire request flow across services
- Identify bottlenecks and latency issues
- Debug inter-service communication problems

---

## Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| API Response Time (p95) | < 200ms | For GET requests |
| Order Creation Time (p95) | < 500ms | Including DB writes |
| Driver Assignment Time (p95) | < 2s | Including algorithm execution |
| Database Query Time (p95) | < 50ms | For most queries |
| gRPC Call Latency (p95) | < 100ms | Inter-service communication |
| Throughput | 1000 orders/sec | Peak load handling |
| Availability | 99.9% | Maximum 8.7 hours downtime/year |

---

## Testing Strategy

### Unit Tests
- Test business logic in isolation
- Mock gRPC/database calls
- Target: 80% code coverage

### Integration Tests
- Test service-to-service communication
- Use test containers for PostgreSQL
- Verify gRPC contracts

### End-to-End Tests
- Simulate complete user journeys
- Test workflow executions
- Verify data consistency

### Load Tests
- Simulate 10,000 concurrent users
- Test database connection pooling
- Identify breaking points

---

## Future Enhancements

1. **Real-time Notifications**: WebSocket/SSE for live updates
2. **Payment Integration**: Stripe/PayPal integration
3. **Promotions Engine**: Coupon codes, discounts, loyalty points
4. **Analytics Service**: Customer behavior, restaurant performance
5. **ML-based ETA**: Machine learning for accurate delivery predictions
6. **Multi-tenant**: Support multiple restaurant chains
7. **Mobile SDK**: Native iOS/Android SDKs
8. **GraphQL Gateway**: Alternative to REST for mobile clients

---

## Conclusion

This microservices architecture provides a solid foundation for a scalable food delivery platform. The separation of concerns across Data, API, and Orchestration services ensures maintainability and allows independent scaling based on load patterns.

**Key Strengths**:
- Clear service boundaries and responsibilities
- Single database owner pattern prevents data inconsistencies
- Workflow orchestration enables complex business logic
- gRPC for efficient inter-service communication
- Extensible design for future enhancements

**Implementation Timeline**:
- Week 1-2: Data Service + Database schema
- Week 3-4: API Service + HTTP endpoints
- Week 5-6: Orchestration Service + Workflows
- Week 7-8: Integration, testing, deployment

---

**Document Version**: 1.0
**Last Updated**: 2025-01-18
**Prepared By**: Claude Code
**Status**: Ready for Implementation
