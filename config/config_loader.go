package config

import (
	"time"
	"github.com/prometheus/common/model"
	"github.com/prometheus/alertmanager/constants"
)

// ConfigLoader loads config for co-ordinator
type ConfigLoader interface {
	Load(c *Config) error
}

// InitConfig returns a config at the time of initialization
func InitConfig() *Config {
	global := initGlobal()

	return &Config {
		Global: global,
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

func initGlobal() *GlobalConfig {
	global := DefaultGlobalConfig()
	
	resolveMinutes := constants.GetOrDefaultEnvInt("ALERTMANAGER_RESOLVE_TIMEOUT", 5)
	global.ResolveTimeout = model.Duration(time.Duration(resolveMinutes) * time.Minute)

	global.SMTPSmarthost = HostPort {
		Host: constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_HOST", "localhost"),
		Port: constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_PORT", "25"),
	}

	global.SMTPFrom = constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_FROM","alertmanager@signoz.io")
	
	return &global
}
