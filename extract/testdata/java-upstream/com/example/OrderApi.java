package com.example;

import org.springframework.web.bind.annotation.*;
import java.util.List;

@RequestMapping("/api/v1/orders")
public interface OrderApi {

    @GetMapping
    List<OrderDto> listOrders(@RequestParam(value = "offset", defaultValue = "0") int offset);

    @PostMapping
    OrderDto createOrder(@RequestBody CreateOrderRequest request);
}

class OrderDto {
    private String id;
}

class CreateOrderRequest {
    private String name;
}
