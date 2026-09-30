package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
)

// DeviceAuthorization is a started device grant (RFC 8628): the person approves
// UserCode at VerificationURIComplete while the client polls with DeviceCode.
type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

func decodeProtocol[T any](value any) (out T, err error) {
	data, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return
}

// StartDeviceAuthorization begins a device grant for clientID. preferredContext
// is "account" or "system": the authority the person is asked to approve.
func (c *Client) StartDeviceAuthorization(ctx context.Context, clientID, preferredContext string) (DeviceAuthorization, error) {
	value, err := c.OAuth().DeviceAuthorization(ctx, url.Values{"client_id": {clientID}, "preferred_context": {preferredContext}})
	if err != nil {
		return DeviceAuthorization{}, err
	}
	device, err := decodeProtocol[DeviceAuthorization](value)
	if err != nil {
		return device, err
	}
	if device.DeviceCode == "" || device.ExpiresIn <= 0 {
		return device, fmt.Errorf("invalid device authorization response")
	}
	if device.VerificationURIComplete == "" {
		device.VerificationURIComplete = device.VerificationURI
	}
	return device, nil
}

// AwaitDeviceToken polls until the person approves or denies the grant, or it
// expires. It honours the server's interval and slows down when asked to.
func (c *Client) AwaitDeviceToken(ctx context.Context, clientID string, device DeviceAuthorization) (model.TokenResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(device.ExpiresIn)*time.Second)
	defer cancel()
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return model.TokenResponse{}, ctx.Err()
		case <-timer.C:
		}
		tokens, err := c.token(ctx, url.Values{"client_id": {clientID}, "grant_type": {deviceGrantType}, "device_code": {device.DeviceCode}})
		if err == nil {
			return tokens, nil
		}
		var problem *Problem
		if !errors.As(err, &problem) {
			return tokens, err
		}
		switch problem.Code {
		case "slow_down":
			interval += 5 * time.Second
		case "authorization_pending":
		default:
			return tokens, err
		}
	}
}

// RefreshToken exchanges a refresh token for a new pair. The old refresh
// token is spent.
func (c *Client) RefreshToken(ctx context.Context, clientID, refreshToken string) (model.TokenResponse, error) {
	return c.token(ctx, url.Values{"client_id": {clientID}, "grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

// RevokeToken ends the session token belongs to. Revoking an unknown or
// already revoked token succeeds.
func (c *Client) RevokeToken(ctx context.Context, clientID, token string) error {
	_, err := c.OAuth().Revoke(ctx, url.Values{"client_id": {clientID}, "token": {token}})
	return err
}

func (c *Client) token(ctx context.Context, form url.Values) (model.TokenResponse, error) {
	value, err := c.OAuth().Token(ctx, form)
	if err != nil {
		return model.TokenResponse{}, err
	}
	return decodeProtocol[model.TokenResponse](value)
}
