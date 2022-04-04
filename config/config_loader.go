package config

import (
	"io/ioutil"
	"path/filepath"
	"gopkg.in/yaml.v2"
)

// ConfigLoader loads config for co-ordinator
type ConfigLoader interface {
	Load(c *Config) error
}


// configFileLoader is default config loader that reads
// from yaml file. This is primarily meant for test coverage
type configFileLoader struct {
	filePath string
}

func NewConfigFileLoader(filePath string) ConfigLoader {
	return &configFileLoader{
		filePath: filePath,
	}
}

func (cfl *configFileLoader) Load(c *Config) error {
	content, err := ioutil.ReadFile(cfl.filePath)
	if err != nil {
		return err
	}

	err = yaml.UnmarshalStrict(content, c)
	if err != nil {
		return err
	}
	
	c.original = string(content)
	resolveFilepaths(filepath.Dir(cfl.filePath), c)
	return c.Validate()
}

