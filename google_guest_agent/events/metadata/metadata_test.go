// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
)

var (
	errUnknown = fmt.Errorf("simple error")
)

type mdsClient struct {
	disableUnknownFailure bool
}

func (mds *mdsClient) Get(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Get() not yet implemented")
}

func (mds *mdsClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	return "", fmt.Errorf("GetKey() not yet implemented")
}

func (mds *mdsClient) Watch(ctx context.Context) (*metadata.Descriptor, error) {
	if !mds.disableUnknownFailure {
		return nil, errUnknown
	}
	return nil, nil
}

func (mds *mdsClient) WriteGuestAttributes(ctx context.Context, key string, value string) error {
	return fmt.Errorf("WriteGuestattributes() not yet implemented")
}

func TestWatcherAPI(t *testing.T) {
	watcher := New()
	expectedEvents := []string{LongpollEvent}
	if !reflect.DeepEqual(watcher.Events(), expectedEvents) {
		t.Fatalf("watcher.Events() returned: %+v, expected: %+v.", watcher.Events(), expectedEvents)
	}

	if watcher.ID() != WatcherID {
		t.Errorf("watcher.ID() returned: %s, expected: %s.", watcher.ID(), WatcherID)
	}
}

func TestWatcherSuccess(t *testing.T) {
	watcher := New()
	watcher.client = &mdsClient{disableUnknownFailure: true}

	renew, evData, err := watcher.Run(context.Background(), LongpollEvent)
	if err != nil {
		t.Errorf("watcher.Run(%s) returned error: %+v, expected success.", LongpollEvent, err)
	}

	if !renew {
		t.Errorf("watcher.Run(%s) returned renew: %t, expected: true.", LongpollEvent, renew)
	}

	switch evData.(type) {
	case *metadata.Descriptor:
	default:
		t.Errorf("watcher.Run(%s) returned a non descriptor object.", LongpollEvent)
	}
}

func TestWatcherUnknownFailure(t *testing.T) {
	watcher := New()
	watcher.client = &mdsClient{}

	renew, _, err := watcher.Run(context.Background(), LongpollEvent)
	if err == nil {
		t.Errorf("watcher.Run(%s) returned no error, expected: %v.", LongpollEvent, errUnknown)
	}

	if !renew {
		t.Errorf("watcher.Run(%s) returned renew: %t, expected: true.", LongpollEvent, renew)
	}
}
