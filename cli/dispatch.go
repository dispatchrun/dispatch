package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type dispatchApi struct {
	client *http.Client
	apiKey string
}

func (d *dispatchApi) ListSigningKeys() (*ListSigningKeys, error) {
	req, err := http.NewRequest(
		"POST",
		DispatchApiUrl+"/dispatch.v1.SigningKeyService/ListSigningKeys",
		bytes.NewBuffer([]byte("{}")),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, authError{}
	case http.StatusOK:
		// continue
	default:
		return nil, errors.New("failed to list signing keys, status: " + resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	skeys := &ListSigningKeys{}
	if err := json.Unmarshal(body, skeys); err != nil {
		return nil, err
	}
	return skeys, nil
}

func (d *dispatchApi) CreateSigningKey() (*SigningKey, error) {
	req, err := http.NewRequest(
		"POST",
		DispatchApiUrl+"/dispatch.v1.SigningKeyService/CreateSigningKey",
		bytes.NewBuffer([]byte("{}")),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, authError{}
	case http.StatusOK:
		// continue
	default:
		return nil, errors.New("failed to list signing keys, status: " + resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	skey := &SigningKey{}
	if err := json.Unmarshal(body, skey); err != nil {
		return nil, err
	}
	return skey, nil
}
