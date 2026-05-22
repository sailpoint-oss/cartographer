import { ApiProperty } from '@nestjs/swagger';

export class UserDto {
  @ApiProperty({ name: 'user_name' })
  userName!: string;

  displayName!: string;
}
