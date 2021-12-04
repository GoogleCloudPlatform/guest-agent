package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

const metadataURLPrefix = "http://metadata.google.internal/computeMetadata/v1/instance/"

// GetMetadataHTTPResponse returns http response for the specified key without checking status code.
func GetMetadataHTTPResponse(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s%s", metadataURLPrefix, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func GetMetadata(path string) (string, error) {
	resp, err := GetMetadataHTTPResponse(path)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http response code is %v", resp.StatusCode)
	}
	val, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(val), nil
}
