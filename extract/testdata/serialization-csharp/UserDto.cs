namespace Example;

using System.Text.Json.Serialization;

public record UserDto(
    [property: JsonPropertyName("user_id")] string UserId,
    string DisplayName
);
