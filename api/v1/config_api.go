package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/notify/webhook"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/common/model"
)

// file_name: config_api.go
// description: contains methods (extensions) to support dynamic config and reload

// addRoute includes new routes in configuration and reloads alert manager
// the assumption is receiver can have max one route
// because routes dont have unique keys we rely on receiver names
// for updates.
// channel - route - receiver (one to one mapping)
func (api *API) addRoute(w http.ResponseWriter, req *http.Request) {

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	receiver := config.Receiver{}
	if err := json.Unmarshal(body, &receiver); err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}
	if receiver.Name == "" {
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}

	cr := config.ConfigChangeRequest{
		Action:   config.AddRouteAction,
		Receiver: &receiver,
		Route: &config.Route{
			Receiver: receiver.Name,
			Continue: true,
		},
	}

	if err := cr.Validate(); err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to update channel (%s)", receiver.Name))
		return
	}

	// write Route to disk
	api.updateConfigCh <- &cr

	if err := <-api.updateConfigErrCh; err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to update channel (%s)", receiver.Name))
		return
	}

	api.respond(w, nil)
}

// editRoute re-writes route and receiver configuration.
// The operation replaces route and receiver records hence
// all attributes of Route and Receiver would be required
// inputs: Route, Receiver
// the operation also reloads the alert manager
func (api *API) editRoute(w http.ResponseWriter, req *http.Request) {

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	receiver := config.Receiver{}
	if err := json.Unmarshal(body, &receiver); err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	if receiver.Name == "" {
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}

	cr := config.ConfigChangeRequest{
		Action:   config.EditRouteAction,
		Receiver: &receiver,
		Route: &config.Route{
			Receiver: receiver.Name,
			Continue: true,
		},
	}

	// write route and reload config
	api.updateConfigCh <- &cr

	if err := <-api.updateConfigErrCh; err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to update channel (%s)", receiver.Name))
	}
	api.respond(w, nil)
}

// deleteRoute removes the receiver record and currespoding
// routes from config
// the operation also reloads the alert manager
// input : {name: <receiver_name>}
func (api *API) deleteRoute(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	receiver := config.Receiver{}
	if err := json.Unmarshal(body, &receiver); err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	if receiver.Name == "" {
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}

	cr := config.ConfigChangeRequest{
		Action:   config.DeleteRouteAction,
		Receiver: &receiver,
	}

	// write Route to disk
	api.updateConfigCh <- &cr

	if err := <-api.updateConfigErrCh; err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to delete channel (%s)", receiver.Name))
	}
	api.respond(w, nil)
}

func (api *API) testReceiver(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	receiver := config.Receiver{}
	if err := json.Unmarshal(body, &receiver); err != nil {
		api.respondError(w, apiError{typ: errorBadData, err: err}, nil)
		return
	}

	if receiver.Name == "" {
		receiver.Name = fmt.Sprintf("receiver-test-%d", rand.Intn(1000))
	}

	tmpl, err := template.FromGlobs(api.config.Templates...)
	if err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, "failed to parse template from config")
		return
	}

	// todo: get alert manager url
	tmpl.ExternalURL, _ = url.Parse("http://localhost:9093")

	getDummyAlert := func() types.Alert {
		return types.Alert{
			Alert: model.Alert{
				Labels: model.LabelSet{
					"alertname": "TestAlert",
					"severity":  "info",
				},
				Annotations: model.LabelSet{
					"description": "Test Alert fired from SigNoz dashboard",
					"summary":     "Test Alert fired from SigNoz dashboard",
				},
			},
		}
	}
	getCtx := func(receiverName string) context.Context {
		ctx := context.Background()
		ctx = notify.WithGroupKey(ctx, "1")
		ctx = notify.WithRepeatInterval(ctx, time.Hour)
		ctx = notify.WithGroupLabels(ctx, model.LabelSet{
			"alertname": "TestAlert",
			"severity":  "info",
		})
		ctx = notify.WithReceiverName(ctx, receiverName)
		ctx = notify.WithRepeatInterval(ctx, time.Hour)
		return ctx
	}

	if receiver.WebhookConfigs != nil {
		notifier, err := webhook.New(receiver.WebhookConfigs[0], tmpl, api.logger)
		if err != nil {
			api.respondError(w, apiError{err: err, typ: errorInternal}, "failed to prepare message for select config")
			return
		}
		ctx := getCtx(receiver.Name)
		dummyAlert := getDummyAlert()
		_, err = notifier.Notify(ctx, &dummyAlert)
		if err != nil {
			api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to send test message to channel (%s)", receiver.Name))
			return
		}
	}

	api.respond(w, nil)
}
