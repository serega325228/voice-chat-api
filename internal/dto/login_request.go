package dto

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=128"`
}
