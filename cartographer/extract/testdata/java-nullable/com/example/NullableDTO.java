package com.example;

import javax.annotation.Nullable;
import javax.annotation.Nonnull;

public class NullableDTO {
    @Nonnull
    private String id;

    @Nullable
    private String description;

    @Nullable
    private String optionalField;

    private String normalField;
}
