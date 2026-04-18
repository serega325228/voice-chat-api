package models

import "time"

type Session struct {
	Clients   []*Client
	Creator   *Client
	CreatedAt time.Time
	ClosedAt  time.Time
}
