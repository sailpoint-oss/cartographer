package com.example;

public class OuterDTO {
    private String id;
    private Operator operator;
    private Category category;
    private InnerConfig config;

    public enum Operator {
        AND, OR, NOT
    }

    public enum Category {
        HIGH, MEDIUM, LOW
    }

    public static class InnerConfig {
        private String key;
        private String value;
    }
}
