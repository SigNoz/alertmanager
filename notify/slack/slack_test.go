// Copyright 2019 Prometheus Team
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

package slack

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/go-kit/log"
	commoncfg "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify/test"
)

func TestSlackRetry(t *testing.T) {
	notifier, err := New(
		&config.SlackConfig{
			HTTPConfig: &commoncfg.HTTPClientConfig{},
		},
		test.CreateTmpl(t),
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	for statusCode, expected := range test.RetryTests(test.DefaultRetryCodes()) {
		actual, _ := notifier.retrier.Check(statusCode, nil)
		require.Equal(t, expected, actual, fmt.Sprintf("error on status %d", statusCode))
	}
}

func TestSlackRedactedURL(t *testing.T) {
	ctx, u, fn := test.GetContextWithCancelingURL()
	defer fn()

	notifier, err := New(
		&config.SlackConfig{
			APIURL:     &config.URL{URL: u},
			HTTPConfig: &commoncfg.HTTPClientConfig{},
		},
		test.CreateTmpl(t),
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	test.AssertNotifyLeaksNoSecret(t, ctx, notifier, u.String())
}

func TestGettingSlackURLFromFile(t *testing.T) {
	ctx, u, fn := test.GetContextWithCancelingURL()
	defer fn()

	f, err := ioutil.TempFile("", "slack_test")
	require.NoError(t, err, "creating temp file failed")
	_, err = f.WriteString(u.String())
	require.NoError(t, err, "writing to temp file failed")

	notifier, err := New(
		&config.SlackConfig{
			APIURLFile: f.Name(),
			HTTPConfig: &commoncfg.HTTPClientConfig{},
		},
		test.CreateTmpl(t),
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	test.AssertNotifyLeaksNoSecret(t, ctx, notifier, u.String())
}
