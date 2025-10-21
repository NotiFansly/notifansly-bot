package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

//type AccountInfo struct {
//	ID       string `json:"id"`
//	Username string `json:"username"`
//	// Add other fields as needed
//}

func (c *Client) GetAccountInfo(username string) (*ModelAccountInfo, error) {
	url := fmt.Sprintf("%s/api/v1/account?usernames=%s&ngsw-bypass=true", c.BaseURL, username)
	//fmt.Printf("Creator Request Url: %v\n", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	//fmt.Printf("Creator Response: %v", resp)

	var result struct {
		Success  bool               `json:"success"`
		Response []ModelAccountInfo `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.Success || len(result.Response) == 0 {
		return nil, fmt.Errorf("failed to get account info for %s", username)
	}

	if len(result.Response) == 0 {
		return nil, fmt.Errorf("no account info found for %s", username)
	}

	return &result.Response[0], nil
}

func (c *Client) GetAccountInfoByID(fanslyId string) (*ModelAccountInfo, error) {
	url := fmt.Sprintf("%s/api/v1/account?ids=%s&ngsw-bypass=true", c.BaseURL, fanslyId)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result struct {
		Success  bool               `json:"success"`
		Response []ModelAccountInfo `json:"response"`
		Error    *FanslyError       `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if !result.Success {
		if result.Error != nil {
			return nil, fmt.Errorf("API error (code %d): %s", result.Error.Code, result.Error.Details)
		}
		return nil, fmt.Errorf("API request failed without error details")
	}

	if len(result.Response) == 0 {
		return nil, fmt.Errorf("no account info found for ID %s", fanslyId)
	}

	return &result.Response[0], nil
}
