package dto

type WSResponse struct {
	Status WSResponseStatus `json:"status"`
	Error  string           `json:"error,omitempty"`
}
