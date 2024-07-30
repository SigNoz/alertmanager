// Copyright 2023 Prometheus Team
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

package msteams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

const (
	colorRed   = "Attention"
	colorGreen = "Good"
	colorGrey  = "Warning"
)

var (
	// these are redundant with the existing information in the alert
	LabelsToSkip = []string{"alertname", "severity", "ruleId", "ruleSource"}
	// to avoid message restrictions on payload size
	AnnotationsToSkip = []string{"summary", "related_logs", "related_traces"}
)

type Notifier struct {
	conf         *config.MSTeamsConfig
	tmpl         *template.Template
	logger       log.Logger
	client       *http.Client
	retrier      *notify.Retrier
	webhookURL   *config.SecretURL
	postJSONFunc func(ctx context.Context, client *http.Client, url string, body io.Reader) (*http.Response, error)
}

type Content struct {
	Schema  string   `json:"$schema"`
	Type    string   `json:"type"`
	Version string   `json:"version"`
	Body    []Body   `json:"body"`
	Actions []Action `json:"actions"`
}

type Fact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type Body struct {
	Type                string `json:"type"`
	Text                string `json:"text"`
	Weight              string `json:"weigth,omitempty"`
	Size                string `json:"size,omitempty"`
	Wrap                bool   `json:"wrap,omitempty"`
	Style               string `json:"style,omitempty"`
	Color               string `json:"color,omitempty"`
	HorizontalAlignment string `json:"horizontalAlignment,omitempty"`
	Facts               []Fact `json:"facts,omitempty"`
}

type Action struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Attachment struct {
	ContentType string  `json:"contentType"`
	ContentURL  *string `json:"contentUrl"`
	Content     Content `json:"content"`
}

// Adaptive card reference can be found at https://learn.microsoft.com/en-us/power-automate/overview-adaptive-cards
type teamsMessage struct {
	Type        string       `json:"type"`
	Attachments []Attachment `json:"attachments"`
}

// New returns a new notifier that uses the Microsoft Teams Webhook API.
func New(c *config.MSTeamsConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "msteams", httpOpts...)
	if err != nil {
		return nil, err
	}

	n := &Notifier{
		conf:         c,
		tmpl:         t,
		logger:       l,
		client:       client,
		retrier:      &notify.Retrier{RetryCodes: []int{429}},
		webhookURL:   c.WebhookURL,
		postJSONFunc: notify.PostJSON,
	}

	return n, nil
}

func addToBody(body []Body, alert *types.Alert) []Body {
	body = append(body, Body{
		Type:   "TextBlock",
		Text:   "Labels",
		Weight: "Bolder",
		Size:   "Medium",
	})
	facts := []Fact{}
	for k, v := range alert.Labels {
		if slices.Contains(LabelsToSkip, string(k)) {
			continue
		}
		facts = append(facts, Fact{Title: string(k), Value: string(v)})
	}
	body = append(body, Body{
		Type:  "FactSet",
		Facts: facts,
	})

	body = append(body, Body{
		Type:   "TextBlock",
		Text:   "Annotations",
		Weight: "Bolder",
		Size:   "Medium",
	})
	annotationsFacts := []Fact{}
	for k, v := range alert.Annotations {
		if slices.Contains(AnnotationsToSkip, string(k)) {
			continue
		}
		annotationsFacts = append(annotationsFacts, Fact{Title: string(k), Value: string(v)})
	}
	body = append(body, Body{
		Type:  "FactSet",
		Facts: annotationsFacts,
	})
	return body
}

func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	key, err := notify.ExtractGroupKey(ctx)
	if err != nil {
		return false, err
	}

	level.Debug(n.logger).Log("incident", key)

	data := notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
	tmpl := notify.TmplText(n.tmpl, data, &err)
	if err != nil {
		return false, err
	}

	title := tmpl(n.conf.Title)
	if err != nil {
		return false, err
	}

	alerts := types.Alerts(as...)
	color := colorGrey
	switch alerts.Status() {
	case model.AlertFiring:
		color = colorRed
	case model.AlertResolved:
		color = colorGreen
	}

	var ruleSource string
	for _, alert := range as {
		for k, v := range alert.Labels {
			if k == "ruleSource" {
				ruleSource = string(v)
			}
		}
	}

	t := teamsMessage{
		Type: "message",
		Attachments: []Attachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				ContentURL:  nil,
				Content: Content{
					Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
					Type:    "AdaptiveCard",
					Version: "1.2",
					Body: []Body{
						{
							Type:   "TextBlock",
							Text:   title,
							Weight: "Bolder",
							Size:   "Medium",
							Wrap:   true,
							Style:  "heading",
							Color:  color,
						},
					},
					Actions: []Action{
						{
							Type:  "Action.OpenUrl",
							Title: "View Alert",
							URL:   ruleSource,
						},
					},
				},
			},
		},
	}

	for _, alert := range as {
		if alert.Status() == model.AlertFiring {
			t.Attachments[0].Content.Body = addToBody(t.Attachments[0].Content.Body, alert)
		}
		if alert.Status() == model.AlertResolved {
			t.Attachments[0].Content.Body = append(t.Attachments[0].Content.Body, Body{
				Type:   "TextBlock",
				Text:   "Resolved Alerts",
				Weight: "Bolder",
				Size:   "Medium",
				Wrap:   true,
				Color:  colorGreen,
			})
			t.Attachments[0].Content.Body = addToBody(t.Attachments[0].Content.Body, alert)
		}
	}

	var payload bytes.Buffer
	if err = json.NewEncoder(&payload).Encode(t); err != nil {
		return false, err
	}

	resp, err := n.postJSONFunc(ctx, n.client, n.webhookURL.String(), &payload)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	// https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/connectors-using?tabs=cURL#rate-limiting-for-connectors
	retry, err := n.retrier.Check(resp.StatusCode, resp.Body)
	if err != nil {
		reasonErr := notify.NewErrorWithReason(notify.GetFailureReason(resp.StatusCode, fmt.Sprintf("%v", err.Error())), err)
		return retry, reasonErr
	}
	return false, nil
}
