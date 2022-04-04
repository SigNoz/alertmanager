package queryservice

import (
	"fmt"
	"io"
	"strings"
	"net/http"
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/alertmanager/config"
)

type configLoader struct {
	queryServiceURL string
	channelURL string 
	logger   log.Logger
}

func NewConfigLoader(url *string, logger log.Logger) (*configLoader, error) {
	var queryServiceURL string 

	if url == nil {
		return nil, fmt.Errorf("query service url is required for fetching stored config")
	}
	queryServiceURL = *url 

	if !strings.HasSuffix(queryServiceURL, "/") {
		queryServiceURL = queryServiceURL + "/"
	} 
	
	return &configLoader {
		queryServiceURL: queryServiceURL,
		channelURL: queryServiceURL + "api/v1/channels",
		logger: logger,
	}, nil
}

func (cl *configLoader) Load(c *config.Config) error {
	level.Debug(cl.logger).Log("msg", "Config from query service")
	err := cl.prepare(c)
	if err != nil {
		return err
	}
	
	err = c.Validate()

	return err
}

func (cl *configLoader) prepare(c *config.Config) error {
	channels, err := cl.getChannels()
	
	if err != nil {
		return errors.Wrap(err, "received an error from query service while fetching config")
	}

	if len(channels) == 0 {
		level.Warn(cl.logger).Log("msg", "No channels found in query service ")
		return nil
	}

	// channelErr captures the last occurred error (if any)
	var channelErr error 
	
	addRoute := func (data []byte, c *config.Config) error {
		receiver := config.Receiver{}
		err := json.Unmarshal(data, &receiver)
		if err != nil {
			return errors.Wrap(err, "failed to marshal receiver from query service")
		}
		route := config.Route{}
		err = json.Unmarshal(data, &route)

		if route.Receiver == "" {
			route.Receiver = receiver.Name
		}

		err = c.AddRoute(&route, &receiver)
		if err != nil {
			return errors.Wrap(err, "failed to add route")
		}
		return nil
	} 

	for _, ch := range channels {
		err := addRoute([]byte(ch.Data), c)
		if err != nil {
			level.Error(cl.logger).Log(
				"msg", "failed to load some of the chanels",
				"channel", ch.Name)
			channelErr = err
		}
	}

	return channelErr
}

func (cl *configLoader) getChannels() ([]channelItem, error) {
	var result  []channelItem

	resp, err := http.Get(cl.channelURL)
	
	if err != nil {
		return nil, errors.Wrap(err, "error in http get")
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
		
	if err != nil {
		return result, errors.Wrap(err, "failed to read response body") 
	}

	var apiResponse channelResponse
	err = json.Unmarshal(body, &apiResponse)
	
	if err != nil {
		level.Error(cl.logger).Log("msg", "failed to unmarshal api response", "response", body, "api", cl.channelURL)
		return result, errors.Wrap(err, "failed to unmarshal api response") 
	}
	
	channelData :=  apiResponse.Data
	level.Debug(cl.logger).Log("msg", "channels data received from query service", "data", channelData)

	if len(channelData) == 0 {
		return result, nil
	} 
 
	return channelData , err
}