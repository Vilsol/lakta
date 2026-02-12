package main

import (
	v1 "github.com/Vilsol/lakta/examples/microservices/gen/go/example/v1"
	"github.com/Vilsol/lakta/pkg/lakta"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/do/v2"
)

const (
	deliveryFeeCents = 500
	taxPercent       = 0.1 // 10%
)

func registerRoutes(app *fiber.App) {
	app.Get("/restaurants", getRestaurants)
	app.Get("/restaurants/:id", getRestaurant)
	app.Get("/restaurants/:id/menu", getRestaurantMenu)

	app.Post("/orders", postOrder)
	app.Get("/orders/:id", getOrder)
	app.Get("/customers/:id/orders", getCustomerOrders)
}

func getRestaurants(c fiber.Ctx) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	resp, err := client.ListRestaurants(c.Context(), &v1.ListRestaurantsRequest{
		Limit:  uint64(fiber.Query[int](c, "limit", 20)),
		Offset: uint64(fiber.Query[int](c, "offset", 0)),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"restaurants": resp.GetRestaurants(),
	})
}

func getRestaurant(c fiber.Ctx) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	resp, err := client.GetRestaurant(c.Context(), &v1.GetRestaurantRequest{
		Id: c.Params("id"),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"restaurant": resp.GetRestaurant(),
	})
}

func getRestaurantMenu(c fiber.Ctx) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	resp, err := client.GetMenu(c.Context(), &v1.GetMenuRequest{
		RestaurantId: c.Params("id"),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"items": resp.GetItems(),
	})
}

type order struct {
	CustomerId   string `json:"customer_id"`
	RestaurantId string `json:"restaurant_id"`
	Items        []struct {
		MenuItemId string `json:"menu_item_id"`
		Quantity   int32  `json:"quantity"`
	} `json:"items"`
	DeliveryAddress string `json:"delivery_address"`
}

func postOrder(c fiber.Ctx) error {
	dataClient, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	workflowClient, err := do.Invoke[v1.WorkflowServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	o := new(order)
	if err := c.Bind().Body(o); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	menu, err := dataClient.GetMenu(c.Context(), &v1.GetMenuRequest{
		RestaurantId: o.RestaurantId,
	})
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "menu not found",
		})
	}

	// Remap order items
	itemMap := make(map[string]int)
	for i, item := range menu.GetItems() {
		itemMap[item.Id] = i
	}

	subtotal := int64(0)
	orderItems := make([]*v1.OrderItemInput, len(o.Items))
	for i, item := range o.Items {
		orderItems[i] = &v1.OrderItemInput{
			MenuItemId: item.MenuItemId,
			Quantity:   item.Quantity,
		}

		oi := menu.GetItems()[itemMap[item.MenuItemId]]

		subtotal += int64(item.Quantity) * oi.PriceCents
	}

	tax := int64(float64(subtotal) * taxPercent)
	total := subtotal + deliveryFeeCents + tax

	resp, err := dataClient.CreateOrder(c.Context(), &v1.CreateOrderRequest{
		CustomerId:       o.CustomerId,
		RestaurantId:     o.RestaurantId,
		Items:            orderItems,
		DeliveryAddress:  o.DeliveryAddress,
		SubtotalCents:    subtotal,
		DeliveryFeeCents: deliveryFeeCents,
		TaxCents:         tax,
		TotalCents:       total,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if _, err := workflowClient.StartOrderWorkflow(c.Context(), &v1.StartOrderWorkflowRequest{
		OrderId: resp.GetOrder().GetId(),
	}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"order": resp.GetOrder(),
		"items": resp.GetItems(),
	})
}

func getOrder(c fiber.Ctx) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	resp, err := client.GetOrder(c.Context(), &v1.GetOrderRequest{
		Id: c.Params("id"),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"order":      resp.GetOrder(),
		"items":      resp.GetItems(),
		"restaurant": resp.GetRestaurant(),
		"customer":   resp.GetCustomer(),
	})
}

func getCustomerOrders(c fiber.Ctx) error {
	client, err := do.Invoke[v1.DataServiceClient](lakta.GetInjector(c.Context()))
	if err != nil {
		return err
	}

	resp, err := client.ListCustomerOrders(c.Context(), &v1.ListCustomerOrdersRequest{
		CustomerId: c.Params("id"),
		Limit:      uint64(fiber.Query[int](c, "limit", 20)),
		Offset:     uint64(fiber.Query[int](c, "offset", 0)),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"orders": resp.GetOrders(),
		"total":  resp.GetTotal(),
	})
}

// fiber:context-methods migrated
