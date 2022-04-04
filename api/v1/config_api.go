package v1

import (
	"fmt"
	"io/ioutil"
	"encoding/json"
	"net/http"
	"github.com/prometheus/alertmanager/config"
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
	if receiver.Name == ""{
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}
	
	cr := config.ConfigChangeRequest{
		Action: config.AddRouteAction, 
		Receiver: &receiver,
		Route: &config.Route {
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

	if receiver.Name == ""{
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}
	
	cr := config.ConfigChangeRequest{
		Action: config.EditRouteAction, 
		Receiver: &receiver,
		Route: &config.Route {
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

	if receiver.Name == ""{
		api.respondError(w, apiError{typ: errorBadData, err: fmt.Errorf("missing receiver name ")}, nil)
		return
	}
	
	cr := config.ConfigChangeRequest{
		Action: config.DeleteRouteAction, 
		Receiver: &receiver,
	}

	// write Route to disk 
	api.updateConfigCh <- &cr

	if err := <-api.updateConfigErrCh; err != nil {
		api.respondError(w, apiError{err: err, typ: errorInternal}, fmt.Sprintf("failed to delete channel (%s)", receiver.Name))
	} 
	api.respond(w, nil)
}