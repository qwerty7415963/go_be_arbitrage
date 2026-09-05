package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

type Meta struct {
	Cursor  string `json:"cursor,omitempty"`
	HasMore bool   `json:"has_more"`
	Limit   int    `json:"limit,omitempty"`
}

type ErrorBody struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

type FieldError struct {
	Field      string      `json:"field"`
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Constraint interface{} `json:"constraint,omitempty"`
}

func RespondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

func RespondCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, Response{
		Success: true,
		Data:    data,
	})
}

func RespondNoContent(c *gin.Context) {
	c.JSON(http.StatusNoContent, nil)
}

func RespondList(c *gin.Context, data interface{}, meta *Meta) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
		Meta:    meta,
	})
}

func RespondError(c *gin.Context, err *domain.AppError) {
	statusCode := err.StatusCode
	if statusCode == 0 {
		statusCode = err.StatusCodeFromCode()
	}

	requestID, _ := c.Get("request_id")
	requestIDStr, _ := requestID.(string)

	c.JSON(statusCode, Response{
		Success: false,
		Error: &ErrorBody{
			Code:      string(err.Code),
			Message:   err.Message,
			Details:   err.Details,
			RequestID: requestIDStr,
		},
	})
}

func RespondValidationError(c *gin.Context, fieldErrors []FieldError) {
	requestID, _ := c.Get("request_id")
	requestIDStr, _ := requestID.(string)

	c.JSON(http.StatusBadRequest, Response{
		Success: false,
		Error: &ErrorBody{
			Code:      string(domain.ErrCodeValidation),
			Message:   "validation failed",
			Details:   fieldErrors,
			RequestID: requestIDStr,
		},
	})
}

func RespondUpstreamError(c *gin.Context, upstream string, err error) {
	appErr := domain.WrapError(domain.ErrCodeExchangeConnectionFailed, "upstream error", err)
	appErr = appErr.WithDetails(map[string]interface{}{
		"upstream": upstream,
	})
	RespondError(c, appErr)
}
