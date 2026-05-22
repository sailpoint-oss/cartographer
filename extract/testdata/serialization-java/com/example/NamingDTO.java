package com.example;

import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import com.fasterxml.jackson.databind.annotation.JsonNaming;

@JsonNaming(PropertyNamingStrategies.SnakeCaseStrategy.class)
public class NamingDTO {
    private String userName;
    private String customId;

    @com.fasterxml.jackson.annotation.JsonProperty("custom_id")
    public String getCustomId() { return customId; }
    public void setCustomId(String customId) { this.customId = customId; }

    public String getUserName() { return userName; }
    public void setUserName(String userName) { this.userName = userName; }
}
