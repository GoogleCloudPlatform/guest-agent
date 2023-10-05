// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package fakes implements fake services for unit testing.
package fakes

import (
	"context"
	"fmt"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
)

type contextKey int

const (
	// MDSOverride is the context key used by fake MDS for getting test conditions.
	MDSOverride contextKey = iota
)

// MDSClient implements fake metadata server.
type MDSClient struct{}

// NewFakeMDSClient returns fake type MDSClient.
func NewFakeMDSClient() *MDSClient {
	return &MDSClient{}
}

// GetKeyRecursive implements fake GetKeyRecursive MDS method.
func (s MDSClient) GetKeyRecursive(ctx context.Context, key string) (string, error) {
	return "", fmt.Errorf("GetKeyRecursive() not yet implemented")
}

// GetKey implements fake GetKey MDS method.
func (s MDSClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	valid := `
	{
		"encrypted_credentials": "q3u9avkCiXCgKAopiG3WFKmIfwidMq+ISLEIufPDBq0EdVRt+5XnEqz1dJyNuqdeRNmP24VlsXaZ77wQtF/6qcg4t0JhUqn18VkodIUvhz8zFdYGe9peu5EprcC/h8MvSrKXS6WmWRn1920/itPo4yPKl31mOGaOwRuPYqNLVUUu1iFZZ3VZTTDp5yh3AyvLoO41UoKi6siZM+xo+PB+qoHcARGctvNfsZv+jZYbAh6PRuJ2kI4aBBp2sUFWQhAZOoDYqLpcrtTe1d9LeQC/PN/PVz5FiLOwu87YsnOGgt7/K1ce2AxDGRJaINHarricVXaqx38h0u8zei7ynTsSZIemNo9SoR6dH7feRaSiH23htHryJQMx8TV32XHzuE0GdApTLkHIqc0eZGmoJ/PGYy6INaVC+kpk+7tlZ3ZwkKneXgroyy20Iig+wfKMcj8i7ncLP01PMep9d7uFaCuoshdxJbAEeqPCNr59D7zfRBDg+QBavLKv3aPSMqFOYF1tqj2mOB1EHsasZgtDslSwDN7EhkR2YbBi2HNSNFKzEnh5SsbXINSyAgaffoK+99YrLRXCQpdaqr9GIRug6HzMzQMsXhIxr4yErVbpPcv7GSC21vi4PWU62zhvWUZ8w4HXds3HjvpJk3ILrglM72xfkddEdr1Hd7KP1F3h6nG+9FFP4s6Z6j7uHPrL+ppd7Od4dDc05hA+Unifoyshb+IaCJGtzewQtofLhyZcoEZBzp1iMT5IwSCZm6eHSwCG9hS7S9eKJAcjLBwSxWZhwO4UXU3mJM0ZTZfxUxXtmR9Ombpm5xpIu5fa4rMi1DUCKK2vrYDR5hYJrEUsFLzyK+4EGuWz+FPgMXi6gXMZZYVQCjS3zcnfBsEL18EvlDHs2muuHWE/gEjGO0nFCUFuNwkOY2bW+BU8/eKwosYxYhQk+jwYJFEuSXqtm+wgCEyFvIbg42GDc+YrKPTxAzWiBH/RL/XrPR4InDZ6extmSYZbneLjT1YRAAfLR/MOiWuY2I38Q2VYBzMqZ6y1/1EgToNMW2viYlxEVmN1ys0msospzxCGwlR0DWkSzEDJmYT2SQcKFC9OrdMZ2o6BD4s315M8lv5v7ZsL7KuoYNZ4gMBN6MrxJYD6OwdLeytCmI71LdvgVw5gdDmoChu9dFDyzPKSoMYJnvTr5ktrYwxZIyWn8Sl3BjAaslZkAwL+c5oijCTCZ+oV9vzdD7tBnFx9y3fVVFtMC3nflyEjInEUPCupxh38O4TsYLLVl7tttL696kUKdlHL1SRAFCX1Wb5p4WNSBzQQtTGU1dsw904CncAj32sW32oGFWqb4Bom1OzoV/equ32Anef8J95mF+ahmf1BvTUMUq5Az2mSi2/dFBhuhy7rhGQyVWpwCEzpzVpVlysDr5aWr8CLbDOLzJv3MIDM3QQ=",
		"key_import_blob": {
			"duplicate": "ACAFYwCs8qzuSCCTvS1iCIHVTDuEXrP7WNNYPGl44ZPARLbhYVWaSkttYk1J2ChEEwG+u0fRxBVF95nEbe3xzN17+pppFFKelB9Jlf+PybtE0rRMyIJ0CB4HT9w=",
			"encrypted_seed": "ACBnqcxLycU+VUxeB89a7DCa0BSqOciydCReXia87EDLjQAgEUyXgTSjqA4tOxRNARnW5fw4B2p6AJFLD1nZx+llJP8=",
			"public_area": "AAgACwAAAEAAAAAQACCmhjk4ZFa6nbv58ya74lshnfNfGaCta6+hPIR5s+hZBw=="
		}
	}
	`

	invalid := `
	{
		"encrypted_credentials": "q3u9avkCLOwu87YsnOmNo9SoR6d/dFBhuhy7rhGQyVWpwCEzpzVpVlysDr5aWr8CLbDOLzJv3MIDM3QQ=",
		"key_import_blob": {
			"duplicate": "ACAFYwCs8qzuSCCTvS1iCIHVT9Jlf+PybtE0rRMyIJ0CB4HT9w=",
			"encrypted_seed": "ACBnqcxLycU+VUxeB8D1nZx+llJP8=",
			"public_area": "AAgACwAAAEAAAA+hPIR5s+hZBw=="
		}
	}
	`

	switch ctx.Value(MDSOverride) {
	case "succeed":
		return valid, nil
	case "fail_mds_connect":
		return "", fmt.Errorf("this is fake MDS error")
	case "fail_unmarshal":
		return invalid, nil
	default:
		return "", nil
	}
}

// Get method implements fake Get on MDS.
func (s MDSClient) Get(context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// Watch method implements fake watcher on MDS.
func (s MDSClient) Watch(context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// WriteGuestAttributes method implements fake writer on MDS.
func (s MDSClient) WriteGuestAttributes(context.Context, string, string) error {
	return fmt.Errorf("not yet implemented")
}
