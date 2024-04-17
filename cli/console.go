package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type console struct{}

func (c *console) Login(token string) error {
	clilogin := &clilogin{}

	for {
		url := fmt.Sprintf("%s/cli-login/token", DispatchConsoleUrl)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		values := req.URL.Query()
		values.Add("token", token)
		req.URL.RawQuery = values.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// If 204, the token was not created yet, retry
		if resp.StatusCode == http.StatusNoContent {
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("login failed with status %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(data, clilogin); err != nil {
			return fmt.Errorf("failed to unmarshal login response: %w", err)
		}
		break
	}

	var config Config
	config.Warning = "THIS FILE IS GENERATED. DO NOT EDIT!"
	config.Organization = map[string]Organization{}

	for i, org := range clilogin.Organizations {
		config.Organization[org.Slug] = Organization{APIKey: org.ApiKey}
		if i == 0 {
			config.Active = org.Slug
		}
	}

	if err := CreateConfig(DispatchConfigPath, &config); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	return nil
}

type clilogin struct {
	Organizations []struct {
		Slug   string `json:"slug"`
		ApiKey string `json:"api_key"`
	} `json:"organizations"`
}
