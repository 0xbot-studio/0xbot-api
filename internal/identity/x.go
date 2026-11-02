package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type XClient struct {
	ID, Secret, Callback string
	HTTP                 *http.Client
}

func (x XClient) Exchange(ctx context.Context, code, verifier string) (string, string, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {x.Callback}, "code_verifier": {verifier}, "client_id": {x.ID}}
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.x.com/2/oauth2/token", strings.NewReader(form.Encode()))
	if e != nil {
		return "", "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if x.Secret != "" {
		req.SetBasicAuth(x.ID, x.Secret)
	}
	client := x.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, e := client.Do(req)
	if e != nil {
		return "", "", ErrAuth
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", "", ErrAuth
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&tokens); e != nil || tokens.AccessToken == "" {
		return "", "", ErrAuth
	}
	req, e = http.NewRequestWithContext(ctx, "GET", "https://api.x.com/2/users/me", nil)
	if e != nil {
		return "", "", e
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	profile, e := client.Do(req)
	if e != nil {
		return "", "", ErrAuth
	}
	defer profile.Body.Close()
	if profile.StatusCode != 200 {
		return "", "", ErrAuth
	}
	var p struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if e = json.NewDecoder(io.LimitReader(profile.Body, 64<<10)).Decode(&p); e != nil || p.Data.ID == "" {
		return "", "", errors.New("x_profile_unavailable")
	}
	return p.Data.ID, p.Data.Name, nil
}
