from pydantic import BaseModel, Field


class UserDto(BaseModel):
    user_id: str = Field(serialization_alias="userId")
    display_name: str
