package config

import (
	"time"
	"github.com/prometheus/common/model"
)

// ConfigLoader loads config for co-ordinator
type ConfigLoader interface {
	Load(c *Config) error
}

// InitConfig returns a config at the time of initialization
func InitConfig() *Config {
	global:= DefaultGlobalConfig()
	
	global.ResolveTimeout = model.Duration(1 * time.Minute)

	global.SMTPSmarthost = HostPort {
		Host: "localhost",
		Port: "25",
	}
	global.SMTPFrom = "alertmanager@signoz.io"
	return &Config {
		Global: &global,
		Route: &Route{
			Receiver: "default-receiver",
		},
		Receivers: []*Receiver{
			&Receiver{
				Name: "default-receiver", 
				EmailConfigs: []*EmailConfig{
					&EmailConfig{
						NotifierConfig: NotifierConfig{
							VSendResolved: false,
						},
						To: "default@email.com",
						From: "alertmanager@example.org",
						HTML: DefaultEmailConfig.HTML,
				},
			},
		},
	},
	}
}
