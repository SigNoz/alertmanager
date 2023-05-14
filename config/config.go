// Copyright 2015 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	"github.com/prometheus/alertmanager/constants"
	"github.com/prometheus/alertmanager/pkg/labels"
	"github.com/prometheus/alertmanager/timeinterval"
)

const secretToken = "<secret>"

var secretTokenJSON string

func init() {
	b, err := json.Marshal(secretToken)
	if err != nil {
		panic(err)
	}
	secretTokenJSON = string(b)
}

// Secret is a string that must not be revealed on marshaling.
type Secret string

// MarshalYAML implements the yaml.Marshaler interface for Secret.
func (s Secret) MarshalYAML() (interface{}, error) {
	if s != "" {
		return secretToken, nil
	}
	return nil, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Secret.
func (s *Secret) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Secret
	return unmarshal((*plain)(s))
}

// MarshalJSON implements the json.Marshaler interface for Secret.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(secretToken)
}

// URL is a custom type that represents an HTTP or HTTPS URL and allows validation at configuration load time.
type URL struct {
	*url.URL
}

// Copy makes a deep-copy of the struct.
func (u *URL) Copy() *URL {
	v := *u.URL
	return &URL{&v}
}

// MarshalYAML implements the yaml.Marshaler interface for URL.
func (u URL) MarshalYAML() (interface{}, error) {
	if u.URL != nil {
		return u.URL.String(), nil
	}
	return nil, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for URL.
func (u *URL) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	urlp, err := parseURL(s)
	if err != nil {
		return err
	}
	u.URL = urlp.URL
	return nil
}

// MarshalJSON implements the json.Marshaler interface for URL.
func (u URL) MarshalJSON() ([]byte, error) {
	if u.URL != nil {
		return json.Marshal(u.URL.String())
	}
	return []byte("null"), nil
}

// UnmarshalJSON implements the json.Marshaler interface for URL.
func (u *URL) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	urlp, err := parseURL(s)
	if err != nil {
		return err
	}
	u.URL = urlp.URL
	return nil
}

// SecretURL is a URL that must not be revealed on marshaling.
type SecretURL URL

// MarshalYAML implements the yaml.Marshaler interface for SecretURL.
func (s SecretURL) MarshalYAML() (interface{}, error) {
	if s.URL != nil {
		return secretToken, nil
	}
	return nil, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for SecretURL.
func (s *SecretURL) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}
	// In order to deserialize a previously serialized configuration (eg from
	// the Alertmanager API with amtool), `<secret>` needs to be treated
	// specially, as it isn't a valid URL.
	if str == secretToken {
		s.URL = &url.URL{}
		return nil
	}
	return unmarshal((*URL)(s))
}

// MarshalJSON implements the json.Marshaler interface for SecretURL.
func (s SecretURL) MarshalJSON() ([]byte, error) {
	return json.Marshal(secretToken)
}

// UnmarshalJSON implements the json.Marshaler interface for SecretURL.
func (s *SecretURL) UnmarshalJSON(data []byte) error {
	// In order to deserialize a previously serialized configuration (eg from
	// the Alertmanager API with amtool), `<secret>` needs to be treated
	// specially, as it isn't a valid URL.
	if string(data) == secretToken || string(data) == secretTokenJSON {
		s.URL = &url.URL{}
		return nil
	}
	return json.Unmarshal(data, (*URL)(s))
}

// Load parses the YAML input s into a Config.
// amol/signoz 22/03/2022 - used only by test cases
func Load(s string) (*Config, error) {
	cfg := &Config{}
	err := yaml.UnmarshalStrict([]byte(s), cfg)
	if err != nil {
		return nil, err
	}
	// Check if we have a root route. We cannot check for it in the
	// UnmarshalYAML method because it won't be called if the input is empty
	// (e.g. the config file is empty or only contains whitespace).
	if cfg.Route == nil {
		return nil, errors.New("no route provided in config")
	}

	// Check if continue in root route.
	if cfg.Route.Continue {
		return nil, errors.New("cannot have continue in root route")
	}

	cfg.original = s

	return cfg, nil
}

// LoadFile parses the given YAML file into a Config.
// amol/signoz 22/03/2022 - used only by test cases
func LoadFile(filename string) (*Config, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	cfg, err := Load(string(content))
	if err != nil {
		return nil, err
	}

	resolveFilepaths(filepath.Dir(filename), cfg)
	return cfg, nil
}

// resolveFilepaths joins all relative paths in a configuration
// with a given base directory.
func resolveFilepaths(baseDir string, cfg *Config) {
	join := func(fp string) string {
		if len(fp) > 0 && !filepath.IsAbs(fp) {
			fp = filepath.Join(baseDir, fp)
		}
		return fp
	}

	for i, tf := range cfg.Templates {
		cfg.Templates[i] = join(tf)
	}

	cfg.Global.HTTPConfig.SetDirectory(baseDir)
	for _, receiver := range cfg.Receivers {
		for _, cfg := range receiver.OpsGenieConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.PagerdutyConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.PushoverConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.SlackConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.VictorOpsConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.WebhookConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.WechatConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.SNSConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
		for _, cfg := range receiver.MSTeamsConfigs {
			cfg.HTTPConfig.SetDirectory(baseDir)
		}
	}
}

// MuteTimeInterval represents a named set of time intervals for which a route should be muted.
type MuteTimeInterval struct {
	Name          string                      `yaml:"name"`
	TimeIntervals []timeinterval.TimeInterval `yaml:"time_intervals"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for MuteTimeInterval.
func (mt *MuteTimeInterval) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain MuteTimeInterval
	if err := unmarshal((*plain)(mt)); err != nil {
		return err
	}
	if mt.Name == "" {
		return fmt.Errorf("missing name in mute time interval")
	}
	return nil
}

// Config is the top-level configuration for Alertmanager's config files.
type Config struct {
	Global       *GlobalConfig  `yaml:"global,omitempty" json:"global,omitempty"`
	Route        *Route         `yaml:"route,omitempty" json:"route,omitempty"`
	InhibitRules []*InhibitRule `yaml:"inhibit_rules,omitempty" json:"inhibit_rules,omitempty"`
	Receivers    []*Receiver    `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	// todo: templates will need a base directory mapping
	// as earlier version depended on the yaml file path to determine
	// base dir but we no longer use yaml file
	Templates         []string           `yaml:"templates" json:"templates"`
	MuteTimeIntervals []MuteTimeInterval `yaml:"mute_time_intervals,omitempty" json:"mute_time_intervals,omitempty"`

	// original is the input from which the config was parsed.
	original string
}

type ConfigOpts struct {
	ResolveTimeout *time.Duration
	GroupInterval  *time.Duration
	GroupWait      *time.Duration
	RepeatInterval *time.Duration
	GroupByStr     []string
}

// InitConfig returns a config at the time of initialization,
// it does not however return a validated config. you must call
// config.validate() to default and valdiate the params
func InitConfig(opts *ConfigOpts) *Config {
	global := DefaultGlobalConfig()

	if opts.ResolveTimeout != nil {
		global.ResolveTimeout = model.Duration(*opts.ResolveTimeout)
	}

	route := &Route{
		Receiver: "default-receiver",
	}

	if len(opts.GroupByStr) > 0 {
		route.GroupByStr = opts.GroupByStr
	} else {
		route.GroupByStr = []string{"alertname"}
	}

	if opts.GroupInterval != nil {
		ginterval := model.Duration(*opts.GroupInterval)
		route.GroupInterval = &ginterval
	}

	if opts.GroupWait != nil {
		gwait := model.Duration(*opts.GroupWait)
		route.GroupWait = &gwait
	}

	if opts.RepeatInterval != nil {
		repeatint := model.Duration(*opts.RepeatInterval)
		route.RepeatInterval = &repeatint
	}

	return &Config{
		Global: &global,
		Route:  route,
		Receivers: []*Receiver{
			&Receiver{
				Name: "default-receiver",
			},
		},
	}
}

func (c Config) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating config string: %s>", err)
	}
	return string(b)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Config.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	return c.Validate()
}

func (c *Config) SetOriginal() error {

	if c == nil {
		return nil
	}

	configBytes, err := json.Marshal(*c)
	if err != nil {
		return err
	}
	c.original = string(configBytes)
	return nil
}

// Validate checks the config and self-corrects whenever possible
func (c *Config) Validate() error {
	// Check if we have a root route. We cannot check for it in the
	// UnmarshalYAML method because it won't be called if the input is empty
	// (e.g. the config file is empty or only contains whitespace).
	if c.Route == nil {
		return errors.New("no route provided in config")
	}

	// Check if continue in root route.
	if c.Route.Continue {
		return errors.New("cannot have continue in root route")
	}

	// If a global block was open but empty the default global config is overwritten.
	// We have to restore it here.
	if c.Global == nil {
		c.Global = &GlobalConfig{}
		*c.Global = DefaultGlobalConfig()
	}

	if c.Global.SlackAPIURL != nil && len(c.Global.SlackAPIURLFile) > 0 {
		return fmt.Errorf("at most one of slack_api_url & slack_api_url_file must be configured")
	}

	if c.Global.OpsGenieAPIKey != "" && len(c.Global.OpsGenieAPIKeyFile) > 0 {
		return fmt.Errorf("at most one of opsgenie_api_key & opsgenie_api_key_file must be configured")
	}

	names := map[string]struct{}{}

	for _, rcv := range c.Receivers {
		if _, ok := names[rcv.Name]; ok {
			return fmt.Errorf("notification config name %q is not unique", rcv.Name)
		}
		for _, wh := range rcv.WebhookConfigs {
			if wh.HTTPConfig == nil {
				wh.HTTPConfig = c.Global.HTTPConfig
			}
			if err := wh.Validate(); err != nil {
				return err
			}
		}
		for _, ec := range rcv.EmailConfigs {

			if ec.Smarthost.String() == "" {
				if c.Global.SMTPSmarthost.String() == "" {
					return fmt.Errorf("no global SMTP smarthost set")
				}
				ec.Smarthost = c.Global.SMTPSmarthost
			}
			if ec.From == "" {
				if c.Global.SMTPFrom == "" {
					return fmt.Errorf("no global SMTP from set")
				}
				ec.From = c.Global.SMTPFrom
			}
			if ec.Hello == "" {
				ec.Hello = c.Global.SMTPHello
			}
			if ec.AuthUsername == "" {
				ec.AuthUsername = c.Global.SMTPAuthUsername
			}
			if ec.AuthPassword == "" {
				ec.AuthPassword = c.Global.SMTPAuthPassword
			}
			if ec.AuthSecret == "" {
				ec.AuthSecret = c.Global.SMTPAuthSecret
			}
			if ec.AuthIdentity == "" {
				ec.AuthIdentity = c.Global.SMTPAuthIdentity
			}
			if ec.RequireTLS == nil {
				ec.RequireTLS = new(bool)
				*ec.RequireTLS = c.Global.SMTPRequireTLS
			}
			if err := ec.Validate(); err != nil {
				return err
			}
		}
		for _, sc := range rcv.SlackConfigs {
			if sc.HTTPConfig == nil {
				sc.HTTPConfig = c.Global.HTTPConfig
			}
			if sc.APIURL == nil && len(sc.APIURLFile) == 0 {
				if c.Global.SlackAPIURL == nil && len(c.Global.SlackAPIURLFile) == 0 {
					return fmt.Errorf("no global Slack API URL set either inline or in a file")
				}
				sc.APIURL = c.Global.SlackAPIURL
				sc.APIURLFile = c.Global.SlackAPIURLFile
			}

			if err := sc.Validate(); err != nil {
				return err
			}
		}
		for _, poc := range rcv.PushoverConfigs {
			if poc.HTTPConfig == nil {
				poc.HTTPConfig = c.Global.HTTPConfig
			}
		}
		for _, pdc := range rcv.PagerdutyConfigs {
			if pdc.HTTPConfig == nil {
				pdc.HTTPConfig = c.Global.HTTPConfig
			}
			if pdc.URL == nil {
				if c.Global.PagerdutyURL == nil {
					return fmt.Errorf("no global PagerDuty URL set")
				}
				pdc.URL = c.Global.PagerdutyURL
			}
		}
		for _, ogc := range rcv.OpsGenieConfigs {
			if ogc.HTTPConfig == nil {
				ogc.HTTPConfig = c.Global.HTTPConfig
			}
			if ogc.APIURL == nil {
				if c.Global.OpsGenieAPIURL == nil {
					return fmt.Errorf("no global OpsGenie URL set")
				}
				ogc.APIURL = c.Global.OpsGenieAPIURL
			}
			if !strings.HasSuffix(ogc.APIURL.Path, "/") {
				ogc.APIURL.Path += "/"
			}
			if ogc.APIKey == "" && len(ogc.APIKeyFile) == 0 {
				if c.Global.OpsGenieAPIKey == "" && len(c.Global.OpsGenieAPIKeyFile) == 0 {
					return fmt.Errorf("no global OpsGenie API Key set either inline or in a file")
				}
				ogc.APIKey = c.Global.OpsGenieAPIKey
				ogc.APIKeyFile = c.Global.OpsGenieAPIKeyFile
			}
		}
		for _, wcc := range rcv.WechatConfigs {
			if wcc.HTTPConfig == nil {
				wcc.HTTPConfig = c.Global.HTTPConfig
			}

			if wcc.APIURL == nil {
				if c.Global.WeChatAPIURL == nil {
					return fmt.Errorf("no global Wechat URL set")
				}
				wcc.APIURL = c.Global.WeChatAPIURL
			}

			if wcc.APISecret == "" {
				if c.Global.WeChatAPISecret == "" {
					return fmt.Errorf("no global Wechat ApiSecret set")
				}
				wcc.APISecret = c.Global.WeChatAPISecret
			}

			if wcc.CorpID == "" {
				if c.Global.WeChatAPICorpID == "" {
					return fmt.Errorf("no global Wechat CorpID set")
				}
				wcc.CorpID = c.Global.WeChatAPICorpID
			}

			if !strings.HasSuffix(wcc.APIURL.Path, "/") {
				wcc.APIURL.Path += "/"
			}
		}
		for _, voc := range rcv.VictorOpsConfigs {
			if voc.HTTPConfig == nil {
				voc.HTTPConfig = c.Global.HTTPConfig
			}
			if voc.APIURL == nil {
				if c.Global.VictorOpsAPIURL == nil {
					return fmt.Errorf("no global VictorOps URL set")
				}
				voc.APIURL = c.Global.VictorOpsAPIURL
			}
			if !strings.HasSuffix(voc.APIURL.Path, "/") {
				voc.APIURL.Path += "/"
			}
			if voc.APIKey == "" {
				if c.Global.VictorOpsAPIKey == "" {
					return fmt.Errorf("no global VictorOps API Key set")
				}
				voc.APIKey = c.Global.VictorOpsAPIKey
			}
		}
		for _, sns := range rcv.SNSConfigs {
			if sns.HTTPConfig == nil {
				sns.HTTPConfig = c.Global.HTTPConfig
			}
		}
		for _, msteams := range rcv.MSTeamsConfigs {
			if msteams.HTTPConfig == nil {
				msteams.HTTPConfig = c.Global.HTTPConfig
			}
			if msteams.WebhookURL == nil {
				return fmt.Errorf("no msteams webhook URL provided")
			}
		}

		names[rcv.Name] = struct{}{}
	}

	// The root route must not have any matchers as it is the fallback node
	// for all alerts.
	if c.Route == nil {
		return fmt.Errorf("no routes provided")
	}
	if len(c.Route.Receiver) == 0 {
		return fmt.Errorf("root route must specify a default receiver")
	}
	if len(c.Route.Match) > 0 || len(c.Route.MatchRE) > 0 {
		return fmt.Errorf("root route must not have any matchers")
	}
	if len(c.Route.MuteTimeIntervals) > 0 {
		return fmt.Errorf("root route must not have any mute time intervals")
	}

	if err := c.Route.Validate(); err != nil {
		return err
	}

	// Validate that all receivers used in the routing tree are defined.
	if err := checkReceiver(c.Route, names); err != nil {
		return err
	}

	tiNames := make(map[string]struct{})
	for _, mt := range c.MuteTimeIntervals {
		if _, ok := tiNames[mt.Name]; ok {
			return fmt.Errorf("mute time interval %q is not unique", mt.Name)
		}
		tiNames[mt.Name] = struct{}{}
	}
	return checkTimeInterval(c.Route, tiNames)
}

// AddRoute adds a new route to configuration.
// the assumption is receiver can have max one route
// This method is intended for local disk updates only
func (c *Config) AddRoute(r *Route, rcv *Receiver) error {

	if rcv == nil || r == nil {
		return fmt.Errorf("adding a route requires both route and receiver")
	}

	if r.Receiver == "" || rcv.Name == "" {
		return fmt.Errorf("receiver is mandatory in route and receiver")
	}

	// check that receiver name is not already used
	for _, receiver := range c.Receivers {
		if receiver.Name == rcv.Name || receiver.Name == r.Receiver {
			return fmt.Errorf("the channel name has to be unique, please choose a different name")
		}
	}
	// must set continue on route to allow subsequent
	// routes to work. may have to rethink on this after
	// adding matchers and filters in upstream
	r.Continue = true

	c.Route.Routes = append(c.Route.Routes, r)
	c.Receivers = append(c.Receivers, rcv)
	return nil
}

// EditRoute changes an existing route in configuration.
// the assumption is receiver can have max one route
// This method is intended for local disk updates only
func (c *Config) EditRoute(r *Route, rcv *Receiver) error {

	if rcv == nil || r == nil {
		return fmt.Errorf("adding a route requires both route and receiver")
	}

	if r.Receiver == "" || rcv.Name == "" {
		return fmt.Errorf("receiver is mandatory in route and receiver")
	}

	// find and update receiver
	for i, receiver := range c.Receivers {
		if receiver.Name == rcv.Name {
			c.Receivers[i] = rcv
		}
	}

	// assumption: the route hierarchy has max 1 level
	// may have to rethink this after adding matchers and filters
	// in upstream. presently we only have top level, no filter routes
	routes := c.Route.Routes
	for i, route := range routes {
		if route.Receiver == r.Receiver {
			c.Route.Routes[i] = r
			break
		}
	}

	return nil
}

// DeleteRoute deletes an existing route in the configuration.
// the assumption is receiver can have max one route and
// the route hierachy has max of 1 level
// This method is intended for local disk file updates only (not in-memory updates)
func (c *Config) DeleteRoute(name string) error {

	if name == "" {
		return fmt.Errorf("delete receiver requires the receiver name")
	}

	// assumption: the route hierarchy has max 1 level
	routes := c.Route.Routes
	for i, r := range routes {
		if r.Receiver == name {
			c.Route.Routes = append(routes[:i], routes[i+1:]...)
			break
		}
	}

	// find receiver and delete
	for i, receiver := range c.Receivers {
		if receiver.Name == name {
			c.Receivers = append(c.Receivers[:i], c.Receivers[i+1:]...)
			break
		}
	}
	return nil
}

// checkReceiver returns an error if a node in the routing tree
// references a receiver not in the given map.
func checkReceiver(r *Route, receivers map[string]struct{}) error {
	for _, sr := range r.Routes {
		if err := checkReceiver(sr, receivers); err != nil {
			return err
		}
	}
	if r.Receiver == "" {
		return nil
	}
	if _, ok := receivers[r.Receiver]; !ok {
		return fmt.Errorf("undefined receiver %q used in route", r.Receiver)
	}
	return nil
}

func checkTimeInterval(r *Route, timeIntervals map[string]struct{}) error {
	for _, sr := range r.Routes {
		if err := checkTimeInterval(sr, timeIntervals); err != nil {
			return err
		}
	}
	if len(r.MuteTimeIntervals) == 0 {
		return nil
	}
	for _, mt := range r.MuteTimeIntervals {
		if _, ok := timeIntervals[mt]; !ok {
			return fmt.Errorf("undefined time interval %q used in route", mt)
		}
	}
	return nil
}

// DefaultGlobalConfig returns GlobalConfig with default values.
func DefaultGlobalConfig() GlobalConfig {
	var defaultHTTPConfig = commoncfg.DefaultHTTPClientConfig

	resolveMinutes := constants.GetOrDefaultEnvInt("ALERTMANAGER_RESOLVE_TIMEOUT", 5)
	resolveTimeout := model.Duration(time.Duration(resolveMinutes) * time.Minute)

	defaultSMTPSmarthost := HostPort{
		Host: constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_HOST", "localhost"),
		Port: constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_PORT", "25"),
	}

	defaultSMTPFrom := constants.GetOrDefaultEnv("ALERTMANAGER_SMTP_FROM", "alertmanager@signoz.io")

	return GlobalConfig{
		ResolveTimeout:  resolveTimeout,
		HTTPConfig:      &defaultHTTPConfig,
		SMTPSmarthost:   defaultSMTPSmarthost,
		SMTPFrom:        defaultSMTPFrom,
		SMTPHello:       "localhost",
		SMTPRequireTLS:  true,
		PagerdutyURL:    mustParseURL("https://events.pagerduty.com/v2/enqueue"),
		OpsGenieAPIURL:  mustParseURL("https://api.opsgenie.com/"),
		WeChatAPIURL:    mustParseURL("https://qyapi.weixin.qq.com/cgi-bin/"),
		VictorOpsAPIURL: mustParseURL("https://alert.victorops.com/integrations/generic/20131114/alert/"),
	}
}

func mustParseURL(s string) *URL {
	u, err := parseURL(s)
	if err != nil {
		panic(err)
	}
	return u
}

func parseURL(s string) (*URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q for URL", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host for URL")
	}
	return &URL{u}, nil
}

// HostPort represents a "host:port" network address.
type HostPort struct {
	Host string
	Port string
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for HostPort.
func (hp *HostPort) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var (
		s   string
		err error
	)
	if err = unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	hp.Host, hp.Port, err = net.SplitHostPort(s)
	if err != nil {
		return err
	}
	if hp.Port == "" {
		return errors.Errorf("address %q: port cannot be empty", s)
	}
	return nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for HostPort.
func (hp *HostPort) UnmarshalJSON(data []byte) error {
	var (
		s   string
		err error
	)
	if err = json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	hp.Host, hp.Port, err = net.SplitHostPort(s)
	if err != nil {
		return err
	}
	if hp.Port == "" {
		return errors.Errorf("address %q: port cannot be empty", s)
	}
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for HostPort.
func (hp HostPort) MarshalYAML() (interface{}, error) {
	return hp.String(), nil
}

// MarshalJSON implements the json.Marshaler interface for HostPort.
func (hp HostPort) MarshalJSON() ([]byte, error) {
	return json.Marshal(hp.String())
}

func (hp HostPort) String() string {
	if hp.Host == "" && hp.Port == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", hp.Host, hp.Port)
}

// GlobalConfig defines configuration parameters that are valid globally
// unless overwritten.
type GlobalConfig struct {
	// ResolveTimeout is the time after which an alert is declared resolved
	// if it has not been updated.
	ResolveTimeout model.Duration `yaml:"resolve_timeout" json:"resolve_timeout"`

	HTTPConfig *commoncfg.HTTPClientConfig `yaml:"http_config,omitempty" json:"http_config,omitempty"`

	SMTPFrom         string   `yaml:"smtp_from,omitempty" json:"smtp_from,omitempty"`
	SMTPHello        string   `yaml:"smtp_hello,omitempty" json:"smtp_hello,omitempty"`
	SMTPSmarthost    HostPort `yaml:"smtp_smarthost,omitempty" json:"smtp_smarthost,omitempty"`
	SMTPAuthUsername string   `yaml:"smtp_auth_username,omitempty" json:"smtp_auth_username,omitempty"`
	SMTPAuthPassword Secret   `yaml:"smtp_auth_password,omitempty" json:"smtp_auth_password,omitempty"`
	SMTPAuthSecret   Secret   `yaml:"smtp_auth_secret,omitempty" json:"smtp_auth_secret,omitempty"`
	SMTPAuthIdentity string   `yaml:"smtp_auth_identity,omitempty" json:"smtp_auth_identity,omitempty"`
	SMTPRequireTLS   bool     `yaml:"smtp_require_tls" json:"smtp_require_tls,omitempty"`
	// Changing from SecretURL to URL, for supporting persistence of
	// runtime config changes
	SlackAPIURL        *URL   `yaml:"slack_api_url,omitempty" json:"slack_api_url,omitempty"`
	SlackAPIURLFile    string `yaml:"slack_api_url_file,omitempty" json:"slack_api_url_file,omitempty"`
	PagerdutyURL       *URL   `yaml:"pagerduty_url,omitempty" json:"pagerduty_url,omitempty"`
	OpsGenieAPIURL     *URL   `yaml:"opsgenie_api_url,omitempty" json:"opsgenie_api_url,omitempty"`
	OpsGenieAPIKey     Secret `yaml:"opsgenie_api_key,omitempty" json:"opsgenie_api_key,omitempty"`
	OpsGenieAPIKeyFile string `yaml:"opsgenie_api_key_file,omitempty" json:"opsgenie_api_key_file,omitempty"`
	WeChatAPIURL       *URL   `yaml:"wechat_api_url,omitempty" json:"wechat_api_url,omitempty"`
	WeChatAPISecret    Secret `yaml:"wechat_api_secret,omitempty" json:"wechat_api_secret,omitempty"`
	WeChatAPICorpID    string `yaml:"wechat_api_corp_id,omitempty" json:"wechat_api_corp_id,omitempty"`
	VictorOpsAPIURL    *URL   `yaml:"victorops_api_url,omitempty" json:"victorops_api_url,omitempty"`
	VictorOpsAPIKey    Secret `yaml:"victorops_api_key,omitempty" json:"victorops_api_key,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for GlobalConfig.
func (c *GlobalConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultGlobalConfig()
	type plain GlobalConfig
	return unmarshal((*plain)(c))
}

// A Route is a node that contains definitions of how to handle alerts.
type Route struct {
	Receiver string `yaml:"receiver,omitempty" json:"receiver,omitempty"`

	GroupByStr []string          `yaml:"group_by,omitempty" json:"group_by,omitempty"`
	GroupBy    []model.LabelName `yaml:"-" json:"-"`
	GroupByAll bool              `yaml:"-" json:"-"`
	// Deprecated. Remove before v1.0 release.
	Match map[string]string `yaml:"match,omitempty" json:"match,omitempty"`
	// Deprecated. Remove before v1.0 release.
	MatchRE           MatchRegexps `yaml:"match_re,omitempty" json:"match_re,omitempty"`
	Matchers          Matchers     `yaml:"matchers,omitempty" json:"matchers,omitempty"`
	MuteTimeIntervals []string     `yaml:"mute_time_intervals,omitempty" json:"mute_time_intervals,omitempty"`
	Continue          bool         `yaml:"continue" json:"continue,omitempty"`
	Routes            []*Route     `yaml:"routes,omitempty" json:"routes,omitempty"`

	GroupWait      *model.Duration `yaml:"group_wait,omitempty" json:"group_wait,omitempty"`
	GroupInterval  *model.Duration `yaml:"group_interval,omitempty" json:"group_interval,omitempty"`
	RepeatInterval *model.Duration `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"`
}

// Key returns unique identification of route
func (r *Route) Key() string {
	return r.Receiver
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Route.
func (r *Route) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Route
	if err := unmarshal((*plain)(r)); err != nil {
		return err
	}
	return r.Validate()
}

func (r *Route) Validate() error {

	for k := range r.Match {
		if !model.LabelNameRE.MatchString(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
	}

	// make a dictionary of labels
	groupBy := map[model.LabelName]struct{}{}

	// popluate dictionary to de-dup labels
	for _, ln := range r.GroupBy {
		groupBy[ln] = struct{}{}
	}

	for _, l := range r.GroupByStr {
		if l == "..." {
			r.GroupByAll = true
		} else {
			labelName := model.LabelName(l)
			if !labelName.IsValid() {
				return fmt.Errorf("invalid label name %q in group_by list", l)
			}

			if _, ok := groupBy[labelName]; !ok {
				// found new label
				r.GroupBy = append(r.GroupBy, labelName)
				groupBy[labelName] = struct{}{}
			}
		}
	}

	if len(r.GroupBy) > 0 && r.GroupByAll {
		return fmt.Errorf("cannot have wildcard group_by (`...`) and other other labels at the same time")
	}

	if r.GroupInterval != nil && time.Duration(*r.GroupInterval) == time.Duration(0) {
		return fmt.Errorf("group_interval cannot be zero")
	}
	if r.RepeatInterval != nil && time.Duration(*r.RepeatInterval) == time.Duration(0) {
		return fmt.Errorf("repeat_interval cannot be zero")
	}

	return nil
}

func (r *Route) Walk(visit func(*Route)) {
	visit(r)
	if r.Routes == nil {
		return
	}
	for i := range r.Routes {
		r.Routes[i].Walk(visit)
	}
}

// InhibitRule defines an inhibition rule that mutes alerts that match the
// target labels if an alert matching the source labels exists.
// Both alerts have to have a set of labels being equal.
type InhibitRule struct {
	// SourceMatch defines a set of labels that have to equal the given
	// value for source alerts. Deprecated. Remove before v1.0 release.
	SourceMatch map[string]string `yaml:"source_match,omitempty" json:"source_match,omitempty"`
	// SourceMatchRE defines pairs like SourceMatch but does regular expression
	// matching. Deprecated. Remove before v1.0 release.
	SourceMatchRE MatchRegexps `yaml:"source_match_re,omitempty" json:"source_match_re,omitempty"`
	// SourceMatchers defines a set of label matchers that have to be fulfilled for source alerts.
	SourceMatchers Matchers `yaml:"source_matchers,omitempty" json:"source_matchers,omitempty"`
	// TargetMatch defines a set of labels that have to equal the given
	// value for target alerts. Deprecated. Remove before v1.0 release.
	TargetMatch map[string]string `yaml:"target_match,omitempty" json:"target_match,omitempty"`
	// TargetMatchRE defines pairs like TargetMatch but does regular expression
	// matching. Deprecated. Remove before v1.0 release.
	TargetMatchRE MatchRegexps `yaml:"target_match_re,omitempty" json:"target_match_re,omitempty"`
	// TargetMatchers defines a set of label matchers that have to be fulfilled for target alerts.
	TargetMatchers Matchers `yaml:"target_matchers,omitempty" json:"target_matchers,omitempty"`
	// A set of labels that must be equal between the source and target alert
	// for them to be a match.
	Equal model.LabelNames `yaml:"equal,omitempty" json:"equal,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for InhibitRule.
func (r *InhibitRule) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain InhibitRule
	if err := unmarshal((*plain)(r)); err != nil {
		return err
	}

	for k := range r.SourceMatch {
		if !model.LabelNameRE.MatchString(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
	}

	for k := range r.TargetMatch {
		if !model.LabelNameRE.MatchString(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
	}

	return nil
}

// Receiver configuration provides configuration on how to contact a receiver.
type Receiver struct {
	// A unique identifier for this receiver.
	Name string `yaml:"name" json:"name"`

	EmailConfigs     []*EmailConfig     `yaml:"email_configs,omitempty" json:"email_configs,omitempty"`
	PagerdutyConfigs []*PagerdutyConfig `yaml:"pagerduty_configs,omitempty" json:"pagerduty_configs,omitempty"`
	SlackConfigs     []*SlackConfig     `yaml:"slack_configs,omitempty" json:"slack_configs,omitempty"`
	WebhookConfigs   []*WebhookConfig   `yaml:"webhook_configs,omitempty" json:"webhook_configs,omitempty"`
	OpsGenieConfigs  []*OpsGenieConfig  `yaml:"opsgenie_configs,omitempty" json:"opsgenie_configs,omitempty"`
	WechatConfigs    []*WechatConfig    `yaml:"wechat_configs,omitempty" json:"wechat_configs,omitempty"`
	PushoverConfigs  []*PushoverConfig  `yaml:"pushover_configs,omitempty" json:"pushover_configs,omitempty"`
	VictorOpsConfigs []*VictorOpsConfig `yaml:"victorops_configs,omitempty" json:"victorops_configs,omitempty"`
	SNSConfigs       []*SNSConfig       `yaml:"sns_configs,omitempty" json:"sns_configs,omitempty"`
	MSTeamsConfigs   []*MSTeamsConfig   `yaml:"msteams_configs,omitempty" json:"msteams_configs,omitempty"`
}

func (c *Receiver) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("receiver name is mandatory")
	}

	if len(c.WebhookConfigs) > 0 {
		for _, w := range c.WebhookConfigs {
			if err := w.Validate(); err != nil {
				return err
			}
		}
	}

	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Receiver.
func (c *Receiver) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Receiver
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	if c.Name == "" {
		return fmt.Errorf("missing name in receiver")
	}
	return nil
}

// MatchRegexps represents a map of Regexp.
type MatchRegexps map[string]Regexp

// UnmarshalYAML implements the yaml.Unmarshaler interface for MatchRegexps.
func (m *MatchRegexps) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain MatchRegexps
	if err := unmarshal((*plain)(m)); err != nil {
		return err
	}
	for k, v := range *m {
		if !model.LabelNameRE.MatchString(k) {
			return fmt.Errorf("invalid label name %q", k)
		}
		if v.Regexp == nil {
			return fmt.Errorf("invalid regexp value for %q", k)
		}
	}
	return nil
}

// Regexp encapsulates a regexp.Regexp and makes it YAML marshalable.
type Regexp struct {
	*regexp.Regexp
	original string
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Regexp.
func (re *Regexp) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	regex, err := regexp.Compile("^(?:" + s + ")$")
	if err != nil {
		return err
	}
	re.Regexp = regex
	re.original = s
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for Regexp.
func (re Regexp) MarshalYAML() (interface{}, error) {
	if re.original != "" {
		return re.original, nil
	}
	return nil, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for Regexp
func (re *Regexp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	regex, err := regexp.Compile("^(?:" + s + ")$")
	if err != nil {
		return err
	}
	re.Regexp = regex
	re.original = s
	return nil
}

// MarshalJSON implements the json.Marshaler interface for Regexp.
func (re Regexp) MarshalJSON() ([]byte, error) {
	if re.original != "" {
		return json.Marshal(re.original)
	}
	return []byte("null"), nil
}

// Matchers is label.Matchers with an added UnmarshalYAML method to implement the yaml.Unmarshaler interface
// and MarshalYAML to implement the yaml.Marshaler interface.
type Matchers labels.Matchers

// UnmarshalYAML implements the yaml.Unmarshaler interface for Matchers.
func (m *Matchers) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var lines []string
	if err := unmarshal(&lines); err != nil {
		return err
	}
	for _, line := range lines {
		pm, err := labels.ParseMatchers(line)
		if err != nil {
			return err
		}
		*m = append(*m, pm...)
	}
	sort.Sort(labels.Matchers(*m))
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface for Matchers.
func (m Matchers) MarshalYAML() (interface{}, error) {
	result := make([]string, len(m))
	for i, matcher := range m {
		result[i] = matcher.String()
	}
	return result, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for Matchers.
func (m *Matchers) UnmarshalJSON(data []byte) error {
	var lines []string
	if err := json.Unmarshal(data, &lines); err != nil {
		return err
	}
	for _, line := range lines {
		pm, err := labels.ParseMatchers(line)
		if err != nil {
			return err
		}
		*m = append(*m, pm...)
	}
	sort.Sort(labels.Matchers(*m))
	return nil
}

// MarshalJSON implements the json.Marshaler interface for Matchers.
func (m Matchers) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("[]"), nil
	}
	result := make([]string, len(m))
	for i, matcher := range m {
		result[i] = matcher.String()
	}
	return json.Marshal(result)
}
