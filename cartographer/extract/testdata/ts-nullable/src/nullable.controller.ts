import { Controller, Get, Param } from '@nestjs/common';

export class NullableDto {
	id: string;

	/** @deprecated Use newField instead */
	oldField?: string;

	description?: string;

	normalField: string;
}

@Controller('api/v1/nullable')
export class NullableController {
	@Get('/:id')
	get(@Param('id') id: string): NullableDto {
		return null;
	}
}
