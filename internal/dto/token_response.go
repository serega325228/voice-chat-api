package dto

type TokenResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}
