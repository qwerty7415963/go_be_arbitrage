package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register godoc
// @Summary      Register new user
// @Description  Create a new user account
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      RegisterRequest  true  "Registration data"
// @Success      201   {object}  api.Response{data=AuthResponse}
// @Failure      400   {object}  api.Response
// @Failure      409   {object}  api.Response
// @Router       /api/v1/auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	resp, err := h.service.Register(c.Request.Context(), &req)
	if err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "registration failed", err))
		}
		return
	}

	c.JSON(http.StatusCreated, api.Response{
		Success: true,
		Data:    resp,
	})
}

// Login godoc
// @Summary      Login
// @Description  Authenticate with email and password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "Login credentials"
// @Success      200   {object}  api.Response{data=AuthResponse}
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	resp, err := h.service.Login(c.Request.Context(), &req)
	if err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "login failed", err))
		}
		return
	}

	api.RespondSuccess(c, resp)
}

// Refresh godoc
// @Summary      Refresh token
// @Description  Get new access token using refresh token
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      RefreshRequest  true  "Refresh token"
// @Success      200   {object}  api.Response{data=AuthResponse}
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/refresh [post]
func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	resp, err := h.service.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "refresh failed", err))
		}
		return
	}

	api.RespondSuccess(c, resp)
}

// Logout godoc
// @Summary      Logout
// @Description  Revoke all refresh tokens for current user
// @Tags         auth
// @Security     BearerAuth
// @Produce      json
// @Success      204  "No Content"
// @Failure      401  {object}  api.Response
// @Router       /api/v1/auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	if err := h.service.Logout(c.Request.Context(), userID); err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "logout failed", err))
		return
	}

	api.RespondNoContent(c)
}

// ChangePassword godoc
// @Summary      Change password
// @Description  Change current user's password
// @Tags         auth
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      ChangePasswordRequest  true  "Password change data"
// @Success      204  "No Content"
// @Failure      400   {object}  api.Response
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/change-password [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	if err := h.service.ChangePassword(c.Request.Context(), userID, req.OldPassword, req.NewPassword); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "change password failed", err))
		}
		return
	}

	api.RespondNoContent(c)
}

// Me godoc
// @Summary      Get current user
// @Description  Get authenticated user's profile
// @Tags         auth
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  api.Response{data=UserResponse}
// @Failure      401  {object}  api.Response
// @Router       /api/v1/auth/me [get]
func (h *Handler) Me(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeNotFound, "user not found"))
		return
	}

	api.RespondSuccess(c, &UserResponse{
		ID:        user.ID.String(),
		Email:     user.Email,
		Role:      user.Role,
		Status:    user.Status,
		CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}
