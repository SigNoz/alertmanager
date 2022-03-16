package v1

import (
	"fmt"
	"net/http"
)
/* This file contains methods (extensions) created by SigNoz */

// addRoute includes new routes in configuration and reloads alert manager 
func (api *API) addRoute(w http.ResponseWriter, req *http.Request) {
	
	// write Route to disk 
	api.saveConfigCh <- RouteAndReceiver{
		&Route{
			Receiver: 'webhook-123'
		},
		&Receiver{
			Name: 'webhook-123',
			WebhookConfigs: []WebhookConfig{
				{
					URL: url.parse("http://localhost:3000/erewr"),
				},
			}
		}
	}

	if err := <-updateConfigErrCh; err != nil {
		// todo: check the response format in case of error 
		http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
	} else {

		// update commplete, lets reload the alert manager 
		errc := make(chan error)
		defer close(errc)

		// send reload signal 
		api.reloadCh <- errc

		if err := <-errc; err != nil {
				http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
		}
	}
	
}
