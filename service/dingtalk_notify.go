/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// dingtalkActionCard is the DingTalk custom-robot "actionCard" message shape.
// A single action card renders a titled markdown body with one full-width
// button, which fits the ban/recover alert: a heading, a detail block, and a
// "view channel status" jump.
type dingtalkActionCard struct {
	MsgType    string `json:"msgtype"`
	ActionCard struct {
		Title          string `json:"title"`
		Text           string `json:"text"`
		SingleTitle    string `json:"singleTitle,omitempty"`
		SingleURL      string `json:"singleURL,omitempty"`
		BtnOrientation string `json:"btnOrientation,omitempty"`
	} `json:"actionCard"`
}

// signDingTalkURL appends the timestamp + HMAC-SHA256 sign query params required
// when the robot has "加签" (signed) security enabled. When secret is empty the
// URL is returned unchanged, supporting robots secured only by keyword/IP.
func signDingTalkURL(webhookURL string, secret string) (string, error) {
	if secret == "" {
		return webhookURL, nil
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
	u, err := url.Parse(webhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid dingtalk webhook url: %v", err)
	}
	q := u.Query()
	q.Set("timestamp", timestamp)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendDingTalkActionCard posts a markdown action card to a DingTalk custom
// robot. It reuses the shared SSRF-protected client (or the worker proxy when
// enabled), signs the URL when a secret is set, and returns an error on any
// transport or non-2xx / non-zero-errcode response so the caller can log it.
func SendDingTalkActionCard(webhookURL string, secret string, title string, markdown string, jumpURL string) error {
	if webhookURL == "" {
		return fmt.Errorf("empty dingtalk webhook url")
	}
	var card dingtalkActionCard
	card.MsgType = "actionCard"
	card.ActionCard.Title = title
	card.ActionCard.Text = markdown
	if jumpURL != "" {
		card.ActionCard.SingleTitle = "查看渠道状态"
		card.ActionCard.SingleURL = jumpURL
	}

	payloadBytes, err := common.Marshal(card)
	if err != nil {
		return fmt.Errorf("failed to marshal dingtalk payload: %v", err)
	}

	finalURL, err := signDingTalkURL(webhookURL, secret)
	if err != nil {
		return err
	}

	var resp *http.Response
	if system_setting.EnableWorker() {
		workerReq := &WorkerRequest{
			URL:    finalURL,
			Key:    system_setting.WorkerValidKey,
			Method: http.MethodPost,
			Headers: map[string]string{
				"Content-Type": "application/json; charset=utf-8",
			},
			Body: payloadBytes,
		}
		resp, err = DoWorkerRequest(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send dingtalk request through worker: %v", err)
		}
		defer resp.Body.Close()
	} else {
		if err := ValidateSSRFProtectedFetchURL(finalURL); err != nil {
			return fmt.Errorf("request reject: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, finalURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			return fmt.Errorf("failed to create dingtalk request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		client := GetSSRFProtectedHTTPClient()
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send dingtalk request: %v", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dingtalk request failed with status code: %d", resp.StatusCode)
	}
	return nil
}
