import { Controller, Get, Post, Body, Param } from '@nestjs/common';

interface UserDto {
  id: string;
  name: string;
  email: string;
}

@Controller('/api/v1/users')
export class UserController {

  @Get()
  async findAll(): Promise<UserDto[]> {
    return [];
  }

  @Get(':id')
  async findOne(@Param('id') id: string): Promise<UserDto> {
    return null;
  }

  @Post()
  async create(@Body() dto: UserDto): Promise<UserDto> {
    return null;
  }
}
