package dto

type RefreshRequest struct {
	Refresh string `json:"refresh" validate:"required"`
}
