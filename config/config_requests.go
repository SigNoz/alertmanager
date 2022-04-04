package config

import "fmt"

const (
	AddRouteAction = iota + 1
	EditRouteAction 
	DeleteRouteAction 
)
// ConfigChangeRequest is useful when managing configuration changes
type ConfigChangeRequest struct {
	Action int 
	Route *Route
	Receiver *Receiver
}

func (c *ConfigChangeRequest) Validate() error { 
	if c.Action == 0 {
		return fmt.Errorf("action field must be set for validating config change request")
	}

	switch c.Action {
		case AddRouteAction, EditRouteAction:
			return c.Receiver.Validate()
		default:
			return nil
	}
}
