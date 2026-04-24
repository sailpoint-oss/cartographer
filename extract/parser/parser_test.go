package parser

import (
	"testing"
)

func TestParseJava(t *testing.T) {
	pool := NewPool()
	if err := pool.RegisterJava(); err != nil {
		t.Fatal(err)
	}

	src := []byte(`
package com.example;

import javax.ws.rs.*;

@RestController
@RequestMapping("/api/users")
public class UserController {

    @GetMapping("/{id}")
    public User getUser(@PathVariable Long id) {
        return userService.findById(id);
    }

    @PostMapping
    public User createUser(@RequestBody CreateUserRequest request) {
        return userService.create(request);
    }
}
`)

	tree, err := pool.Parse("java", src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected non-nil root node")
	}

	// Should have parsed without error
	if root.ChildCount() == 0 {
		t.Fatal("expected children in root node")
	}
}

func TestParseTypeScript(t *testing.T) {
	pool := NewPool()
	if err := pool.RegisterTypeScript(); err != nil {
		t.Fatal(err)
	}

	src := []byte(`
import { Controller, Get, Post, Body, Param } from '@nestjs/common';

@Controller('users')
export class UsersController {
  @Get(':id')
  findOne(@Param('id') id: string): Promise<User> {
    return this.usersService.findOne(id);
  }

  @Post()
  create(@Body() dto: CreateUserDto): Promise<User> {
    return this.usersService.create(dto);
  }
}
`)

	tree, err := pool.Parse("typescript", src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected non-nil root node")
	}
	if root.ChildCount() == 0 {
		t.Fatal("expected children in root node")
	}
}

func TestQueryJavaSpringControllers(t *testing.T) {
	pool := NewPool()
	if err := pool.RegisterJava(); err != nil {
		t.Fatal(err)
	}

	src := []byte(`
package com.example;

@RestController
public class UserController {

    @GetMapping("/{id}")
    public User getUser(@PathVariable Long id) {
        return userService.findById(id);
    }
}
`)

	tree, err := pool.Parse("java", src)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	queryStr, err := LoadQuery("queries/java/spring-controllers.scm")
	if err != nil {
		t.Fatal(err)
	}

	matches, err := pool.Query("java", tree, src, queryStr)
	if err != nil {
		t.Fatal(err)
	}

	// Should find at least the controller class and the endpoint
	if len(matches) == 0 {
		t.Fatal("expected at least one match from spring-controllers query")
	}

	// Check that we found the class name and method name
	foundClass := false
	foundMethod := false
	for _, m := range matches {
		for _, c := range m.Captures {
			if c.Name == "class_name" && c.Text == "UserController" {
				foundClass = true
			}
			if c.Name == "method_name" && c.Text == "getUser" {
				foundMethod = true
			}
		}
	}

	if !foundClass {
		t.Error("expected to find UserController class")
	}
	if !foundMethod {
		t.Error("expected to find getUser method")
	}
}

func TestUnregisteredLanguage(t *testing.T) {
	pool := NewPool()
	_, err := pool.Parse("python", []byte("print('hello')"))
	if err == nil {
		t.Error("expected error for unregistered language")
	}
}
