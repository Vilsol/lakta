package main

import (
	"context"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/google/uuid"
	"github.com/samber/do/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type DataServer struct {
	v1.UnimplementedDataServiceServer
}

func (s *DataServer) ListRestaurants(ctx context.Context, request *v1.ListRestaurantsRequest) (*v1.ListRestaurantsResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	rows, err := db.
		Select("id", "name", "cuisine_type", "address", "created_at", "COUNT(*) OVER() as total_count").
		From("restaurants").
		OrderBy("created_at DESC").
		Limit(request.Limit).
		Offset(request.Offset).
		QueryContext(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}
	defer rows.Close()

	restaurants := make([]*v1.Restaurant, 0)
	var totalCount uint64

	for rows.Next() {
		var restaurant v1.Restaurant
		var createdAt time.Time
		if err = rows.Scan(&restaurant.Id, &restaurant.Name, &restaurant.CuisineType, &restaurant.Address, &createdAt, &totalCount); err != nil {
			return nil, oops.Wrapf(err, "failed to query database")
		}
		restaurant.CreatedAt = timestamppb.New(createdAt)
		restaurants = append(restaurants, &restaurant)
	}

	if err = rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}

	return &v1.ListRestaurantsResponse{
		Restaurants: restaurants,
		Total:       totalCount,
	}, nil
}

func (s *DataServer) GetRestaurant(ctx context.Context, request *v1.GetRestaurantRequest) (*v1.GetRestaurantResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	var restaurant v1.Restaurant
	var createdAt time.Time
	err = db.
		Select("id", "name", "cuisine_type", "address", "created_at").
		From("restaurants").
		Where("id = ?", request.Id).
		QueryRowContext(ctx).
		Scan(&restaurant.Id, &restaurant.Name, &restaurant.CuisineType, &restaurant.Address, &createdAt)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to get restaurant")
	}
	restaurant.CreatedAt = timestamppb.New(createdAt)

	return &v1.GetRestaurantResponse{
		Restaurant: &restaurant,
	}, nil
}

func (s *DataServer) GetMenu(ctx context.Context, request *v1.GetMenuRequest) (*v1.GetMenuResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	rows, err := db.
		Select("id", "restaurant_id", "name", "price", "is_available", "created_at", "COUNT(*) OVER() as total_count").
		From("menu_items").
		OrderBy("created_at DESC").
		Where("restaurant_id = ?", request.RestaurantId).
		QueryContext(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}
	defer rows.Close()

	items := make([]*v1.MenuItem, 0)
	var totalCount uint64

	for rows.Next() {
		var menuItem v1.MenuItem
		var createdAt time.Time
		var price float64
		if err = rows.Scan(&menuItem.Id, &menuItem.RestaurantId, &menuItem.Name, &price, &menuItem.IsAvailable, &createdAt, &totalCount); err != nil {
			return nil, oops.Wrapf(err, "failed to query database")
		}
		menuItem.CreatedAt = timestamppb.New(createdAt)
		menuItem.PriceCents = int64(price * 100) //nolint:mnd
		items = append(items, &menuItem)
	}

	if err = rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}

	return &v1.GetMenuResponse{
		Items: items,
	}, nil
}

func (s *DataServer) GetCustomer(ctx context.Context, request *v1.GetCustomerRequest) (*v1.GetCustomerResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	var customer v1.Customer
	var createdAt time.Time
	err = db.
		Select("id", "email", "name", "created_at").
		From("customers").
		Where("id = ?", request.Id).
		QueryRowContext(ctx).
		Scan(&customer.Id, &customer.Email, &customer.Name, &createdAt)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to get customer")
	}
	customer.CreatedAt = timestamppb.New(createdAt)

	return &v1.GetCustomerResponse{
		Customer: &customer,
	}, nil
}

func (s *DataServer) CreateCustomer(ctx context.Context, request *v1.CreateCustomerRequest) (*v1.CreateCustomerResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	var id uuid.UUID
	var createdAt time.Time
	err = db.Insert("customers").
		Columns("email", "name").
		Values(request.GetEmail(), request.GetName()).
		Suffix("RETURNING id, created_at").
		QueryRowContext(ctx).
		Scan(&id, &createdAt)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to create customer")
	}

	return &v1.CreateCustomerResponse{
		Customer: &v1.Customer{
			Id:        id.String(),
			Email:     request.GetEmail(),
			Name:      request.GetName(),
			CreatedAt: timestamppb.New(createdAt),
		},
	}, nil
}

func (s *DataServer) CreateOrder(ctx context.Context, request *v1.CreateOrderRequest) (*v1.CreateOrderResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	var id uuid.UUID
	var createdAt time.Time
	var status string
	err = db.Insert("orders").
		Columns("customer_id", "restaurant_id", "delivery_address", "subtotal", "delivery_fee", "tax", "total").
		Values(request.GetCustomerId(), request.GetRestaurantId(), request.GetDeliveryAddress(), request.GetSubtotalCents(), request.GetDeliveryFeeCents(), request.GetTaxCents(), request.GetTotalCents()).
		Suffix("RETURNING id, created_at, status").
		QueryRowContext(ctx).
		Scan(&id, &createdAt, &status)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to create order")
	}

	return &v1.CreateOrderResponse{
		Order: &v1.Order{
			Id:               id.String(),
			CustomerId:       request.GetCustomerId(),
			RestaurantId:     request.GetRestaurantId(),
			Status:           v1.OrderStatus(v1.OrderStatus_value["ORDER_STATUS_"+status]),
			DeliveryAddress:  request.GetDeliveryAddress(),
			SubtotalCents:    request.GetSubtotalCents(),
			DeliveryFeeCents: request.GetDeliveryFeeCents(),
			TaxCents:         request.GetTaxCents(),
			TotalCents:       request.GetTotalCents(),
			CreatedAt:        timestamppb.New(createdAt),
		},
	}, nil
}

func (s *DataServer) GetOrder(ctx context.Context, request *v1.GetOrderRequest) (*v1.GetOrderResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	var order v1.Order
	var orderStatus string
	var subtotal float64
	var deliveryFee float64
	var tax float64
	var total float64
	var createdAt time.Time
	var updatedAt time.Time
	err = db.
		Select(
			"id",
			"customer_id",
			"restaurant_id",
			"status",
			"delivery_address",
			"subtotal",
			"delivery_fee",
			"tax",
			"total",
			"created_at",
			"updated_at",
		).
		From("orders").
		Where("id = ?", request.GetId()).
		QueryRowContext(ctx).
		Scan(
			&order.Id,
			&order.CustomerId,
			&order.RestaurantId,
			&orderStatus,
			&order.DeliveryAddress,
			&subtotal,
			&deliveryFee,
			&tax,
			&total,
			&createdAt,
			&updatedAt,
		)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to get order")
	}
	order.Status = v1.OrderStatus(v1.OrderStatus_value[orderStatus])
	order.SubtotalCents = int64(subtotal * 100)       //nolint:mnd
	order.DeliveryFeeCents = int64(deliveryFee * 100) //nolint:mnd
	order.TaxCents = int64(tax * 100)                 //nolint:mnd
	order.TotalCents = int64(total * 100)             //nolint:mnd
	order.CreatedAt = timestamppb.New(createdAt)

	return &v1.GetOrderResponse{
		Order:      &order,
		Items:      nil,
		Customer:   nil,
		Restaurant: nil,
	}, nil
}

func (s *DataServer) UpdateOrderStatus(ctx context.Context, request *v1.UpdateOrderStatusRequest) (*v1.UpdateOrderStatusResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	requestStatus := strings.TrimPrefix(request.GetNewStatus().String(), "ORDER_STATUS_")

	var id uuid.UUID
	var status string
	err = db.Update("orders").
		Set("status", requestStatus).
		Where("id = ?", request.GetOrderId()).
		Suffix("RETURNING id, status").
		QueryRowContext(ctx).
		Scan(&id, &status)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to update order status")
	}

	return &v1.UpdateOrderStatusResponse{
		Status: request.GetNewStatus(),
	}, nil
}

func (s *DataServer) ListCustomerOrders(ctx context.Context, request *v1.ListCustomerOrdersRequest) (*v1.ListCustomerOrdersResponse, error) {
	db, err := do.Invoke[*squirrel.StatementBuilderType](lakta.GetInjector(ctx))
	if err != nil {
		return nil, err
	}

	rows, err := db.
		Select(
			"id",
			"customer_id",
			"restaurant_id",
			"status",
			"delivery_address",
			"subtotal",
			"delivery_fee",
			"tax",
			"total",
			"created_at",
			"updated_at",
			"COUNT(*) OVER() as total_count",
		).
		From("orders").
		OrderBy("created_at DESC").
		Where("customer_id = ?", request.GetCustomerId()).
		QueryContext(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}
	defer rows.Close()

	orders := make([]*v1.Order, 0)
	var totalCount uint64

	for rows.Next() {
		var order v1.Order
		var orderStatus string
		var subtotal float64
		var deliveryFee float64
		var tax float64
		var total float64
		var createdAt time.Time
		var updatedAt time.Time
		if err = rows.Scan(
			&order.Id,
			&order.CustomerId,
			&order.RestaurantId,
			&orderStatus,
			&order.DeliveryAddress,
			&subtotal,
			&deliveryFee,
			&tax,
			&total,
			&createdAt,
			&updatedAt,
			&totalCount,
		); err != nil {
			return nil, oops.Wrapf(err, "failed to query database")
		}
		order.Status = v1.OrderStatus(v1.OrderStatus_value[orderStatus])
		order.SubtotalCents = int64(subtotal * 100)       //nolint:mnd
		order.DeliveryFeeCents = int64(deliveryFee * 100) //nolint:mnd
		order.TaxCents = int64(tax * 100)                 //nolint:mnd
		order.TotalCents = int64(total * 100)             //nolint:mnd
		order.CreatedAt = timestamppb.New(createdAt)
		orders = append(orders, &order)
	}

	if err = rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "failed to query database")
	}

	return &v1.ListCustomerOrdersResponse{
		Orders: orders,
		Total:  totalCount,
	}, nil
}

func NewServer() *DataServer {
	return &DataServer{}
}
