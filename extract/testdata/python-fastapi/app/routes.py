"""User management routes."""
from typing import Annotated, Optional
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, Path, Query, Response
from pydantic import BaseModel, Field

from .auth import get_current_user


router = APIRouter(prefix="/api/v1/users", tags=["users"])


class UserDto(BaseModel):
    """A user in the system."""
    id: UUID
    email: str = Field(..., description="Contact email")
    name: str
    age: int | None = None
    tags: list[str] = []


class CreateUserDto(BaseModel):
    """Payload for creating a user."""
    email: str
    name: str
    age: Optional[int] = None


@router.get("/")
async def list_users(
    response: Response,
    offset: int = 0,
    limit: Annotated[int, Query(le=100)] = 20,
    filter: str | None = None,
    _user=Depends(get_current_user),
) -> list[UserDto]:
    """List all users."""
    response.headers["X-Total-Count"] = "0"
    return []


@router.get("/{user_id}")
async def get_user(user_id: UUID) -> UserDto:
    """Get a user by ID."""
    raise HTTPException(status_code=404)


@router.post("/", status_code=201)
async def create_user(payload: CreateUserDto) -> UserDto:
    """Create a new user."""
    return None


@router.delete("/{user_id}", status_code=204)
async def delete_user(user_id: UUID) -> None:
    """Delete a user."""
    return None
