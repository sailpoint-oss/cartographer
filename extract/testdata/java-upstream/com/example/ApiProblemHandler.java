package com.example;

import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

@ControllerAdvice
public class ApiProblemHandler {

    @ExceptionHandler(NotFoundException.class)
    @ResponseStatus(HttpStatus.NOT_FOUND)
    public ApiErrorDto handleNotFound(NotFoundException ex) {
        return new ApiErrorDto();
    }
}

class NotFoundException extends RuntimeException {}

class ApiErrorDto {
    private String message;
    private int status;
}
