package config

// ConfigLoader loads config for co-ordinator
type ConfigLoader interface {
	Load(c *Config) error
}

// InitConfig returns a config at the time of initialization
func InitConfig() *Config {
	global:= DefaultGlobalConfig()
	
	// tood: fetch this from config file or cli params
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
