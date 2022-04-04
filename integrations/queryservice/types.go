package queryservice

type channelResponse struct {
	Data []channelItem `json:"data" db:"data"`
}

type channelItem struct {
	Id        int       `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Type      string    `json:"type" db:"type"`
	Data      string    `json:"data" db:"data"`
}